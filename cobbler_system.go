package modules

import (
	"context"
	"fmt"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCobblerSystem implements (a subset of) Ansible's
// `cobbler_system` module: adds, edits, removes, or queries a Cobbler
// system record over the Cobbler XML-RPC API — see cobbler_sync.go's
// doc comment for why and how this port reaches that API (a curl
// invocation run via conn.Exec, request/response built/parsed in this
// port's own Go code) rather than an in-process XML-RPC client, and why
// that keeps this module's actual network behavior real rather than
// faked.
//
// Args: host, port, username, password, use_ssl, validate_certs: same
// as cobbler_sync.go. name (string, optional) — the system to
// manage/query. properties (map[string]any, optional) — arbitrary
// Cobbler system attributes (e.g. "profile", "name_servers"), applied
// via `modify_system` one key at a time, matching real cobbler_system.
// interfaces (map[string]map[string]any, optional) — per-device
// interface properties (e.g. interfaces.eth0.macaddress), translated
// to Cobbler's own "<key>-<device>" modify_system keys through the
// same IFPROPS_MAPPING real cobbler_system uses to compare current
// values (see ifpropsMapping below) — the wire key sent to Cobbler is
// the ORIGINAL arg key (e.g. "macaddress-eth0"), not the mapped one;
// the mapping is used only to read the CURRENT value back out of the
// system's own "interfaces" struct for the idempotency comparison,
// exactly like real cobbler_system's own getsystem()/IFPROPS_MAPPING
// use. sync (bool, default false) — calls the `sync` XML-RPC method
// when this task changed anything, matching real cobbler_system's own
// documented behavior (and its own NOTE that concurrent syncs are
// bound to fail). state (present|absent|query, default "present").
//
// Real cobbler_system's own `properties`-key validation only WARNS
// (module.warn) for a key not present on the fetched system, rather
// than failing; this port has no warnings channel on Result (see
// pip_package_info.go's own documented convention for the same gap)
// and instead collects such messages into Extra["warnings"].
//
// This port has no check_mode support at all (a runtime-engine
// concern outside every module's own Func signature here, not specific
// to this module) — real cobbler_system's diff_mode/check_mode output
// is not reproduced.
func moduleCobblerSystem(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	url, insecure := cobblerAPIURL(args)
	username := argString(args, "username", "cobbler")
	password := argString(args, "password", "")
	name := argString(args, "name", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "query" {
		return Result{}, errArg("cobbler_system: state must be present, absent or query, got %q", state)
	}
	properties, _ := args["properties"].(map[string]any)
	interfaces, _ := args["interfaces"].(map[string]any)
	wantSync := argBool(args, "sync", false)

	token, failMsg, err := cobblerLogin(ctx, conn, url, insecure, username, password)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	system, err := cobblerFindSystem(ctx, conn, url, insecure, name, token)
	if err != nil {
		return Result{}, err
	}

	if state == "query" {
		if name != "" {
			return Ok("query").WithExtra("system", system), nil
		}
		systemsVal, err := cobblerCall(ctx, conn, url, insecure, "get_systems", token)
		if err != nil {
			return Fail(fmt.Sprintf("cobbler_system: get_systems failed: %v", err)), nil
		}
		return Ok("query").WithExtra("systems", systemsVal), nil
	}

	changed := false
	var warnings []string

	if state == "absent" {
		if system != nil {
			if _, err := cobblerCall(ctx, conn, url, insecure, "remove_system", name, token); err != nil {
				return Fail(fmt.Sprintf("cobbler_system: failed to remove %s: %v", name, err)), nil
			}
			changed = true
		}
	} else { // present
		if name == "" {
			return Result{}, errArg("cobbler_system: name is required when state is present")
		}
		var systemID any
		if system != nil {
			systemID, err = cobblerSystemHandle(ctx, conn, url, insecure, name, token)
			if err != nil {
				return Fail(fmt.Sprintf("cobbler_system: failed to get handle for %s: %v", name, err)), nil
			}
			for key, value := range properties {
				current, known := system[key]
				if !known {
					warnings = append(warnings, fmt.Sprintf("Property '%s' is not a valid system property.", key))
				}
				if !known || !cobblerValueEqual(current, value) {
					if _, err := cobblerCall(ctx, conn, url, insecure, "modify_system", systemID, key, value, token); err != nil {
						return Fail(fmt.Sprintf("cobbler_system: unable to change '%s' to '%v'. %v", key, value, err)), nil
					}
					changed = true
				}
			}
		} else {
			systemID, err = cobblerCall(ctx, conn, url, insecure, "new_system", token)
			if err != nil {
				return Fail(fmt.Sprintf("cobbler_system: failed to create new system: %v", err)), nil
			}
			if _, err := cobblerCall(ctx, conn, url, insecure, "modify_system", systemID, "name", name, token); err != nil {
				return Fail(fmt.Sprintf("cobbler_system: unable to set name: %v", err)), nil
			}
			changed = true
			for key, value := range properties {
				if _, err := cobblerCall(ctx, conn, url, insecure, "modify_system", systemID, key, value, token); err != nil {
					return Fail(fmt.Sprintf("cobbler_system: unable to change '%s' to '%v'. %v", key, value, err)), nil
				}
			}
		}

		if len(interfaces) > 0 {
			interfaceProperties := map[string]any{}
			var currentInterfaces map[string]any
			if system != nil {
				currentInterfaces, _ = system["interfaces"].(map[string]any)
			}
			for device, rawValues := range interfaces {
				values, _ := rawValues.(map[string]any)
				for key, value := range values {
					if key == "name" {
						continue
					}
					mapped, ok := ifpropsMapping[key]
					if !ok {
						warnings = append(warnings, fmt.Sprintf("Property '%s' is not a valid system property.", key))
						continue
					}
					if system == nil {
						changed = true
					} else {
						devIface, _ := currentInterfaces[device].(map[string]any)
						cur, _ := devIface[mapped]
						if !cobblerValueEqual(cur, value) {
							changed = true
						}
					}
					interfaceProperties[key+"-"+device] = value
				}
			}
			if changed {
				if _, err := cobblerCall(ctx, conn, url, insecure, "modify_system", systemID, "modify_interface", interfaceProperties, token); err != nil {
					return Fail(fmt.Sprintf("cobbler_system: unable to modify interfaces: %v", err)), nil
				}
			}
		}

		if changed {
			if _, err := cobblerCall(ctx, conn, url, insecure, "save_system", systemID, token); err != nil {
				return Fail(fmt.Sprintf("cobbler_system: failed to save %s: %v", name, err)), nil
			}
		}
	}

	if wantSync && changed {
		if _, err := cobblerCall(ctx, conn, url, insecure, "sync", token); err != nil {
			return Fail(fmt.Sprintf("cobbler_system: failed to sync Cobbler. %v", err)), nil
		}
	}

	result := Ok("unchanged")
	if changed {
		result = Changed(name)
	}
	after, err := cobblerFindSystem(ctx, conn, url, insecure, name, token)
	if err != nil {
		return Result{}, err
	}
	result = result.WithExtra("system", after)
	if len(warnings) > 0 {
		result = result.WithExtra("warnings", warnings)
	}
	return result, nil
}

// cobblerFindSystem looks up a system by name via Cobbler's own
// `find_system` XML-RPC method (matching real cobbler_system's own
// getsystem(), which uses find_system rather than get_system — see
// that function's own comment in the real module's source), returning
// nil if name is empty or no system matched.
func cobblerFindSystem(ctx context.Context, conn remoteexec.Connection, url string, insecure bool, name, token string) (map[string]any, error) {
	if name == "" {
		return nil, nil
	}
	result, err := cobblerCall(ctx, conn, url, insecure, "find_system", map[string]any{"name": name}, token)
	if err != nil {
		return nil, fmt.Errorf("cobbler_system: find_system failed: %w", err)
	}
	list, _ := result.([]any)
	if len(list) == 0 {
		return nil, nil
	}
	m, _ := list[0].(map[string]any)
	return m, nil
}

// cobblerSystemHandle returns the internal "handle" modify_system/
// save_system need for an existing system, matching real
// cobbler_system's own version-gated choice between the one-arg and
// two-arg forms of get_system_handle (Cobbler >= 3.4 dropped the token
// argument — see https://github.com/cobbler/cobbler/blame/v3.3.7/cobbler/api.py#L277,
// cited directly in real cobbler_system's own source).
func cobblerSystemHandle(ctx context.Context, conn remoteexec.Connection, url string, insecure bool, name, token string) (any, error) {
	verResult, err := cobblerCall(ctx, conn, url, insecure, "version")
	if err != nil {
		return nil, err
	}
	verStr := fmt.Sprint(verResult)
	verFloat, _ := strconv.ParseFloat(verStr, 64)
	if verFloat >= 3.4 {
		return cobblerCall(ctx, conn, url, insecure, "get_system_handle", name)
	}
	return cobblerCall(ctx, conn, url, insecure, "get_system_handle", name, token)
}

// cobblerValueEqual compares a decoded XML-RPC value (string/int/bool/
// float64/[]any/map[string]any) against a module argument value using
// Go's default equality where possible, falling back to a string
// comparison — real cobbler_system compares Python-native values
// directly (`system[key] != value`); this port's arguments arrive as
// Go's own generic JSON-ish types (see args.go), so an exact type match
// is not guaranteed (e.g. a YAML/JSON int vs an XML-RPC int both decode
// to Go int here, but a list may decode as []string on one side and
// []any on the other) — the string fallback keeps the comparison from
// spuriously reporting "changed" for such a same-value type mismatch,
// at the cost of not catching a genuine type-only difference.
func cobblerValueEqual(a, b any) bool {
	if a == b {
		return true
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// ifpropsMapping mirrors real cobbler_system's own IFPROPS_MAPPING: the
// module argument key (interfaces.<device>.<key>) to the field name
// Cobbler's own "interfaces" struct reports it back under, used only to
// read the CURRENT value for the idempotency comparison — the value
// sent TO Cobbler is always posted under "<key>-<device>" using the
// original (unmapped) key, matching real cobbler_system exactly.
var ifpropsMapping = map[string]string{
	"bondingopts":        "bonding_opts",
	"bridgeopts":         "bridge_opts",
	"connected_mode":     "connected_mode",
	"cnames":             "cnames",
	"dhcptag":            "dhcp_tag",
	"dnsname":            "dns_name",
	"ifgateway":          "if_gateway",
	"interfacetype":      "interface_type",
	"interfacemaster":    "interface_master",
	"ipaddress":          "ip_address",
	"ipv6address":        "ipv6_address",
	"ipv6defaultgateway": "ipv6_default_gateway",
	"ipv6mtu":            "ipv6_mtu",
	"ipv6prefix":         "ipv6_prefix",
	"ipv6secondaries":    "ipv6_secondariesu",
	"ipv6staticroutes":   "ipv6_static_routes",
	"macaddress":         "mac_address",
	"management":         "management",
	"mtu":                "mtu",
	"netmask":            "netmask",
	"static":             "static",
	"staticroutes":       "static_routes",
	"virtbridge":         "virt_bridge",
}
