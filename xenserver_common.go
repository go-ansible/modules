package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// xeBin is the command-line client this port shells out to for every
// xenserver_*.go module in this file's family (xenserver_facts,
// xenserver_guest, xenserver_guest_info, xenserver_guest_powerstate).
// Real community.general's xenserver_* modules are all implemented
// against the XenAPI Python library — a direct XML-RPC binding to
// XenServer/Citrix Hypervisor's own XAPI management daemon — which this
// port has no Go binding for. This port substitutes XenServer's own
// `xe` command-line client instead: the same tool a XenServer
// administrator normally runs by hand, which (like XenAPI itself)
// talks to either a LOCAL XAPI socket when run on the XenServer host's
// own dom0 (matching real xenserver_facts.py's own
// `XenAPI.xapi_local()` local-only design — see xeConnArgs's own doc
// comment) or a REMOTE pool over the network via `-s/-u/-pw` (matching
// real xenserver_guest*.py's own hostname/username/password-driven
// remote XAPI session, normally run with `delegate_to: localhost` from
// the Ansible control node). This mirrors the substitution this
// project already makes for lxd_container.go (pylxd -> `lxc`) and
// udm_*.go (univention.admin -> `udm`). See each module's own doc
// comment for the specific fidelity gaps this implies — `xe`'s own
// plain-text tabular output carries materially less structure than
// XenAPI's native record dicts, so facts gathered through it are a
// best-effort subset, not a byte-exact reproduction of real
// xenserver_*'s own richer "instance" fact tree.
const xeBin = "xe"

// xeConnArgs builds the `-s <host> -u <user> -pw <password>` remote
// connection flags real xenserver_guest*.py's own hostname/username/
// password arguments imply, or none at all when hostname resolves to
// the default "localhost" — in which case `xe` talks to the local XAPI
// socket directly, matching real xenserver_facts.py's own
// connection-argument-free, dom0-local design exactly (xenserver_facts
// has no hostname/username/password options at all: see its own doc
// comment). Real xenserver_guest*.py also falls back to the
// XENSERVER_HOST/XENSERVER_USER/XENSERVER_PASSWORD/
// XENSERVER_VALIDATE_CERTS environment variables when a task omits
// these arguments; this port has no access to the target's own
// environment from a rendered args map, so it applies only the
// documented literal defaults (hostname "localhost", username "root",
// password "") — a caller relying on the environment-variable fallback
// must pass the argument explicitly instead. validate_certs is accepted
// for compatibility but is a no-op: the `xe` binary's own TLS
// verification is not controllable per-invocation the way the XenAPI
// Python binding's ssl context is.
func xeConnArgs(args map[string]any) []string {
	host := argString(args, "hostname", argString(args, "host", argString(args, "pool", "localhost")))
	if host == "" || host == "localhost" {
		return nil
	}
	user := argString(args, "username", argString(args, "admin", argString(args, "user", "root")))
	pass := argString(args, "password", argString(args, "pass", argString(args, "pwd", "")))
	argv := []string{"-s", host, "-u", user}
	if pass != "" {
		argv = append(argv, "-pw", pass)
	}
	return argv
}

// xeRun quotes and runs one `xe` invocation, with args' own connection
// flags (see xeConnArgs) prepended.
func xeRun(ctx context.Context, conn remoteexec.Connection, args map[string]any, sub []string) (remoteexec.Result, error) {
	argv := append([]string{xeBin}, xeConnArgs(args)...)
	argv = append(argv, sub...)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, strings.Join(quoted, " "), nil)
}

// xeFindVMUUID resolves the module's own name(_label)/uuid arguments to
// exactly one VM UUID via `xe vm-list <filter> params=uuid --minimal`,
// matching real get_object_ref()'s own search-then-disambiguate
// behavior: failMsg (with a nil error) is returned instead of a UUID
// when no VM matches, or when name matches more than one VM (uuid
// should be used instead in that case) — both are well-formed "the
// module ran, the request can't be satisfied as given" outcomes, not Go
// errors.
func xeFindVMUUID(ctx context.Context, conn remoteexec.Connection, args map[string]any) (uuid, failMsg string, err error) {
	requestedUUID := argString(args, "uuid", "")
	name := argString(args, "name", argString(args, "name_label", ""))
	if requestedUUID == "" && name == "" {
		return "", "", errArg("one of name (or its alias name_label) or uuid is required")
	}
	var filter string
	if requestedUUID != "" {
		filter = "uuid=" + requestedUUID
	} else {
		filter = "name-label=" + name
	}
	res, err := xeRun(ctx, conn, args, []string{"vm-list", filter, "params=uuid", "--minimal"})
	if err != nil {
		return "", "", fmt.Errorf("xenserver: running vm-list: %w", err)
	}
	if res.RC != 0 {
		return "", "", fmt.Errorf("xenserver: vm-list %s: %s", filter, strings.TrimSpace(res.Stderr))
	}
	var ids []string
	for _, id := range strings.Split(strings.TrimSpace(res.Stdout), ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "", "VM search: No VM found matching name/uuid!", nil
	}
	if len(ids) > 1 {
		return "", fmt.Sprintf("VM search: multiple VMs found matching name %q — use uuid to uniquely specify the VM to manage!", name), nil
	}
	return ids[0], "", nil
}

// xeParamList runs `xe <objType>-param-list uuid=<uuid>` and parses its
// own "<param-name> (<flags>): <value>" plain-text lines into a flat
// map[string]string, one entry per parameter — see xeParseCompound for
// turning a multi-valued (MRW/SRW/SLW, in `xe`'s own flag vocabulary)
// param's own "k1: v1; k2: v2" or "v1; v2" value into a structured
// shape.
func xeParamList(ctx context.Context, conn remoteexec.Connection, args map[string]any, objType, uuid string) (map[string]string, error) {
	res, err := xeRun(ctx, conn, args, []string{objType + "-param-list", "uuid=" + uuid})
	if err != nil {
		return nil, fmt.Errorf("xenserver: running %s-param-list: %w", objType, err)
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("xenserver: %s-param-list uuid=%s: %s", objType, uuid, strings.TrimSpace(res.Stderr))
	}
	return xeParseParamList(res.Stdout), nil
}

func xeParseParamList(out string) map[string]string {
	res := map[string]string{}
	for _, line := range splitLines(out) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parenIdx := strings.Index(trimmed, "(")
		colonIdx := strings.Index(trimmed, "):")
		if parenIdx < 0 || colonIdx < 0 || colonIdx < parenIdx {
			continue
		}
		key := strings.TrimSpace(trimmed[:parenIdx])
		val := ""
		if colonIdx+2 <= len(trimmed) {
			val = strings.TrimSpace(trimmed[colonIdx+2:])
		}
		res[key] = val
	}
	return res
}

// xeParseCompound splits a compound `xe` param value of the form
// "k1: v1; k2: v2" into a map (used for platform/other-config/
// xenstore-data/guest-metrics-networks-style params). A bare
// "v1; v2; v3" list-shaped value (no colons) comes back as a map from
// each value to itself — harmless for the map-typed facts this port
// builds (platform/other_config/xenstore_data), which are always
// genuinely key:value in real XenServer VM records.
func xeParseCompound(s string) map[string]string {
	out := map[string]string{}
	s = strings.TrimSpace(s)
	if s == "" {
		return out
	}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, ":"); idx >= 0 {
			k := strings.TrimSpace(part[:idx])
			v := strings.TrimSpace(part[idx+1:])
			out[k] = v
		} else {
			out[part] = part
		}
	}
	return out
}

// xeParseList splits a `xe` "v1; v2; v3" or "v1,v2,v3" multi-valued
// param (e.g. a --minimal UUID list) into a trimmed, empty-entry-free
// slice.
func xeParseList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	sep := ";"
	if !strings.Contains(s, ";") {
		sep = ","
	}
	var out []string
	for _, part := range strings.Split(s, sep) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// xeModuleVMState maps `xe`'s own power-state vocabulary (running,
// halted, suspended, paused) onto the module-facing names real
// xenserver_*.py itself returns (poweredon, poweredoff, suspended,
// paused), matching xapi_to_module_vm_power_state()'s own table.
func xeModuleVMState(xePowerState string) string {
	switch strings.ToLower(xePowerState) {
	case "running":
		return "poweredon"
	case "halted":
		return "poweredoff"
	case "suspended":
		return "suspended"
	case "paused":
		return "paused"
	default:
		return strings.ToLower(xePowerState)
	}
}

// xeVMFacts gathers a best-effort subset of real gather_vm_facts()'s
// own "instance" fact tree for uuid, via a handful of `xe *-param-list`
// calls (VM, then each referenced VBD/VDI/SR/VIF/network/host). See
// xeBin's own doc comment for why this is necessarily a subset of
// XenAPI's own richer record shape, not a byte-exact reproduction.
//
// Deviations from real gather_vm_facts, all for the same underlying
// reason (no XenAPI guest_metrics/feature-detection available through
// `xe`'s own plain-text param lists without a much larger investment
// this port does not make): customization_agent is always reported as
// "custom" (real xenserver_*.py instead detects "native" for
// XenServer >= 7.0 with guest tools reporting
// feature-static-ip-setting) and every network's prefix/netmask/
// gateway/prefix6/gateway6 are read from the VM's own xenstore-data
// (vm-data/networks/<device>/<field>) — the "custom" agent's own data
// path in real gather_vm_facts — rather than ever trying the "native"
// agent's VIF ipv4_addresses/ipv4_gateway/ipv6_addresses/ipv6_gateway
// fields.
func xeVMFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any, uuid string) (map[string]any, error) {
	vm, err := xeParamList(ctx, conn, args, "vm", uuid)
	if err != nil {
		return nil, err
	}
	platform := xeParseCompound(vm["platform"])
	otherConfig := xeParseCompound(vm["other-config"])
	xenstoreData := xeParseCompound(vm["xenstore-data"])

	numCPUCores := 1
	if v, err := strconv.Atoi(platform["cores-per-socket"]); err == nil {
		numCPUCores = v
	}
	numCPUs := 0
	if v, err := strconv.Atoi(vm["VCPUs-max"]); err == nil {
		numCPUs = v
	}
	memoryMB := 0
	if v, err := strconv.ParseInt(vm["memory-dynamic-max"], 10, 64); err == nil {
		memoryMB = int(v / 1048576)
	}

	homeServer := ""
	if affinity := vm["affinity"]; affinity != "" && affinity != "<not in database>" {
		if hostParams, err := xeParamList(ctx, conn, args, "host", affinity); err == nil {
			homeServer = hostParams["name-label"]
		}
	}

	facts := map[string]any{
		"uuid":        vm["uuid"],
		"name":        vm["name-label"],
		"name_desc":   vm["name-description"],
		"state":       xeModuleVMState(vm["power-state"]),
		"is_template": strings.EqualFold(vm["is-a-template"], "true"),
		"folder":      otherConfig["folder"],
		"domid":       vm["dom-id"],
		"home_server": homeServer,
		"hardware": map[string]any{
			"num_cpus":                 numCPUs,
			"num_cpu_cores_per_socket": numCPUCores,
			"memory_mb":                memoryMB,
		},
		"platform":             stringMapToAny(platform),
		"other_config":         stringMapToAny(otherConfig),
		"xenstore_data":        stringMapToAny(xenstoreData),
		"customization_agent":  "custom",
		"disks":                []any{},
		"cdrom":                map[string]any{},
		"networks":             []any{},
	}

	for _, vbdUUID := range xeParseList(vm["VBDs"]) {
		vbd, err := xeParamList(ctx, conn, args, "vbd", vbdUUID)
		if err != nil {
			continue
		}
		vdiUUID := vbd["vdi-uuid"]
		var vdi map[string]string
		if vdiUUID != "" && vdiUUID != "<not in database>" {
			vdi, _ = xeParamList(ctx, conn, args, "vdi", vdiUUID)
		}
		switch vbd["type"] {
		case "Disk":
			if vdi == nil {
				continue
			}
			sr, _ := xeParamList(ctx, conn, args, "sr", vdi["sr-uuid"])
			facts["disks"] = append(facts["disks"].([]any), map[string]any{
				"size":           vdi["virtual-size"],
				"name":           vdi["name-label"],
				"name_desc":      vdi["name-description"],
				"sr":             sr["name-label"],
				"sr_uuid":        vdi["sr-uuid"],
				"os_device":      vbd["device"],
				"uuid":           vdi["uuid"],
				"vbd_userdevice": vbd["userdevice"],
				"vdi_type":       vdi["type"],
			})
		case "CD":
			if strings.EqualFold(vbd["empty"], "true") || vdi == nil {
				facts["cdrom"] = map[string]any{"type": "none"}
			} else {
				facts["cdrom"] = map[string]any{"type": "iso", "iso_name": vdi["name-label"]}
			}
		}
	}

	for _, vifUUID := range xeParseList(vm["VIFs"]) {
		vif, err := xeParamList(ctx, conn, args, "vif", vifUUID)
		if err != nil {
			continue
		}
		netName := ""
		if netUUID := vif["network-uuid"]; netUUID != "" {
			if net, err := xeParamList(ctx, conn, args, "network", netUUID); err == nil {
				netName = net["name-label"]
			}
		}
		device := vif["device"]
		facts["networks"] = append(facts["networks"].([]any), map[string]any{
			"name":       netName,
			"mac":        vif["MAC"],
			"vif_device": device,
			"mtu":        vif["MTU"],
			"ip":         "",
			"ip6":        []any{},
			"prefix":     xenstoreData["vm-data/networks/"+device+"/prefix"],
			"netmask":    xenstoreData["vm-data/networks/"+device+"/netmask"],
			"gateway":    xenstoreData["vm-data/networks/"+device+"/gateway"],
			"prefix6":    xenstoreData["vm-data/networks/"+device+"/prefix6"],
			"gateway6":   xenstoreData["vm-data/networks/"+device+"/gateway6"],
		})
	}

	return facts, nil
}

func stringMapToAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// xeSetPowerState drives uuid's VM toward power_state (one of
// poweredon|poweredoff|restarted|suspended|shutdownguest|rebootguest —
// already normalized: strip hyphens/underscores and lowercase before
// calling), matching real set_vm_power_state()'s own idempotency rules
// exactly: a state already matching the current one is a no-op
// (changed=false, no command run); restarted/suspended/shutdownguest/
// rebootguest additionally require the VM to already be running (or
// paused, for restarted) and return a non-empty failMsg (not a Go
// error) otherwise, matching real set_vm_power_state()'s own
// module.fail_json calls for those same preconditions. Returns the
// resulting `xe` power-state name (running/halted/suspended) on
// success, mirroring real set_vm_power_state()'s own return value.
func xeSetPowerState(ctx context.Context, conn remoteexec.Connection, args map[string]any, uuid, powerState string, timeout int) (changed bool, resultState, failMsg string, err error) {
	current, err := run(ctx, conn, xeCmdLine(args, []string{"vm-param-get", "uuid=" + uuid, "param-name=power-state"}))
	if err != nil {
		return false, "", "", fmt.Errorf("xenserver: reading power-state: %w", err)
	}
	currentModule := xeModuleVMState(current)

	if currentModule == powerState {
		return false, current, "", nil
	}

	var sub []string
	switch powerState {
	case "poweredon":
		switch currentModule {
		case "poweredoff":
			sub = []string{"vm-start", "uuid=" + uuid}
		case "suspended":
			sub = []string{"vm-resume", "uuid=" + uuid}
		case "paused":
			sub = []string{"vm-unpause", "uuid=" + uuid}
		}
	case "poweredoff":
		sub = []string{"vm-shutdown", "--force", "uuid=" + uuid}
	case "restarted":
		if currentModule != "poweredon" && currentModule != "paused" {
			return false, "", fmt.Sprintf("Cannot restart VM in state '%s'!", currentModule), nil
		}
		sub = []string{"vm-reboot", "--force", "uuid=" + uuid}
	case "suspended":
		if currentModule != "poweredon" {
			return false, "", fmt.Sprintf("Cannot suspend VM in state '%s'!", currentModule), nil
		}
		sub = []string{"vm-suspend", "uuid=" + uuid}
	case "shutdownguest":
		if currentModule != "poweredon" {
			return false, "", fmt.Sprintf("Cannot shutdown guest when VM is in state '%s'!", currentModule), nil
		}
		sub = []string{"vm-shutdown", "uuid=" + uuid}
	case "rebootguest":
		if currentModule != "poweredon" {
			return false, "", fmt.Sprintf("Cannot reboot guest when VM is in state '%s'!", currentModule), nil
		}
		sub = []string{"vm-reboot", "uuid=" + uuid}
	default:
		return false, "", fmt.Sprintf("Requested VM power state '%s' is unsupported!", powerState), nil
	}
	if sub == nil {
		return false, current, "", nil
	}

	res, err := xeRun(ctx, conn, args, sub)
	if err != nil {
		return false, "", "", err
	}
	if res.RC != 0 {
		return false, "", fmt.Sprintf("XAPI ERROR: %s", strings.TrimSpace(res.Stderr)), nil
	}
	newState, err := run(ctx, conn, xeCmdLine(args, []string{"vm-param-get", "uuid=" + uuid, "param-name=power-state"}))
	if err != nil {
		newState = current
	}
	return true, newState, "", nil
}

// xeCmdLine renders one `xe` invocation (with connection flags) as a
// single shell-quoted command line, for use with this package's own
// run()/runStatus() helpers.
func xeCmdLine(args map[string]any, sub []string) string {
	argv := append([]string{xeBin}, xeConnArgs(args)...)
	argv = append(argv, sub...)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// xeWaitForIPDefaultTimeout bounds xeWaitForIP's own poll loop when the
// module's state_change_timeout argument is 0 ("wait indefinitely" in
// real wait_for_vm_ip_address()'s own timeout=0 meaning). This port's
// networks[].ip is only ever populated from the VM's own xenstore-data
// (see xeVMFacts's own doc comment on why it never tries the "native"
// guest-agent IP path) — data nothing inside the guest OS necessarily
// ever writes — so "wait forever" would risk hanging a task
// indefinitely with no way to distinguish "still booting" from "will
// never be written"; this port caps it instead, a deliberate, documented
// narrowing from real wait_for_vm_ip_address()'s own true indefinite
// wait.
const xeWaitForIPDefaultTimeout = 300 * time.Second

// xeWaitForIP polls xeVMFacts's own "networks" once a second until at
// least one network shows a non-empty "ip", or the timeout elapses,
// returning the last-observed facts either way — matching this
// project's own tolerant lxdWaitForIPv4 convention (see
// lxd_container.go's own doc comment) rather than real
// wait_for_vm_ip_address()'s own module.fail_json-on-timeout behavior.
// See xeWaitForIPDefaultTimeout's own doc comment for how timeoutSec<=0
// is handled.
func xeWaitForIP(ctx context.Context, conn remoteexec.Connection, args map[string]any, uuid string, timeoutSec int) (map[string]any, error) {
	timeout := time.Duration(timeoutSec) * time.Second
	if timeoutSec <= 0 {
		timeout = xeWaitForIPDefaultTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		facts, err := xeVMFacts(ctx, conn, args, uuid)
		if err != nil {
			return nil, err
		}
		nets, _ := facts["networks"].([]any)
		haveIP := false
		for _, n := range nets {
			if m, ok := n.(map[string]any); ok {
				if ip, _ := m["ip"].(string); ip != "" {
					haveIP = true
					break
				}
			}
		}
		if haveIP || len(nets) == 0 || time.Now().After(deadline) {
			return facts, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
