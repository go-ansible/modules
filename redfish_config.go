package modules

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRedfishConfig implements Ansible's `redfish_config` module —
// the vendor-NEUTRAL Redfish config module, substituting DMTF's own
// `redfishtool` exactly as redfish_command.go's own doc comment
// describes (genuine remote mode, real credentials via a temp cfgFile).
//
// Real redfish_config.py's own exit_json shape is simpler than
// redfish_command's: `exit_json(changed=result["changed"],
// msg=to_native(result["msg"]))` — no session/return_values extras
// (confirmed by reading real main() FIRST this time, the lesson from
// this batch's own increment-1 return-shape bug). When multiple
// commands are given, real Ansible's own loop keeps going even after a
// failed command and reports only the LAST command's changed/msg —
// this port deliberately does NOT reproduce that (a real, likely
// unintended quirk): a failure here stops the loop and is reported
// immediately, consistent with every other module in this batch.
//
// # This increment's real scope
//
// Real redfish_config.py declares 9 Systems commands, 4 Manager
// commands, and 1 Sessions command. This increment covers 7 of the 9
// Systems commands, each mapped to a real redfishtool operation
// confirmed against redfishtool's own source before writing this file:
//
//   - SetBootOrder / SetPowerRestorePolicy: `redfishtool Systems patch
//     '<json>'` directly — both PATCH the Systems resource itself, no
//     sub-resource discovery needed.
//   - SetDefaultBootOrder / SetBiosDefaultSettings: discover-then-POST
//     via `raw` (see redfishActionsHolder, from redfish_command.go),
//     the same pattern that batch's own ResetToDefaults/SimpleUpdate
//     established — an empty `{}` body, confirmed from real
//     set_default_boot_order/set_bios_default_settings source.
//   - EnableSecureBoot / SetSecureBoot: discover the SecureBoot
//     sub-resource's URI (GET bare Systems, read `SecureBoot.@odata.id`
//     — redfishtool has no named SecureBoot subcommand), then `raw
//     PATCH <uri> -d '{"SecureBootEnable":...}'`.
//   - SetBiosAttributes: discover the Bios sub-resource, GET it,
//     diff requested attributes against its own current values (real
//     idempotency — matches real set_bios_attributes exactly for
//     string/bool attributes; a numeric attribute compared against a
//     differently-typed Go value may not detect "already set" — a
//     real, narrow, disclosed limitation of comparing decoded JSON
//     values without full Redfish type awareness), then PATCH the
//     Bios resource's own `@Redfish.Settings.SettingsObject` URI (NOT
//     the Bios resource itself) with only the changed attributes.
//
// SetBootOrder does not replicate real Ansible's own pre-PATCH
// idempotency check (comparing the requested order against the
// current one) — this port always reports Changed, a real, disclosed,
// narrower gap.
//
// A second increment added the rest of Manager and all of Sessions:
//
//   - SetNetworkProtocols: discover the Manager's own NetworkProtocol
//     sub-resource (GET bare `Managers`, read `NetworkProtocol.@odata.id`
//     — no named redfishtool subcommand for it), then `raw PATCH` with a
//     payload built the same way real set_network_protocols itself
//     normalizes it: ProtocolEnabled coerced from any of
//     true/"true"/"True"/"on"/1 (and the false equivalents) to a real
//     bool, Port coerced to an int, everything else passed through.
//   - SetHostInterface: discover the Manager's HostInterfaces
//     collection, list its members, and select one — by
//     hostinterface_id (matching the URI's own last path segment, same
//     as real Ansible) if given, or the sole member if there is
//     exactly one, else fails loud (real Ansible's own "ID not defined
//     and multiple interfaces detected" case) — then `raw PATCH` the
//     selected member with hostinterface_config verbatim.
//   - SetServiceIdentification / SetSessionService: PATCH via the
//     already-named `Managers`/`SessionService` subcommand's own
//     `patch` operation directly — simplest of this whole batch, no
//     discovery needed since ServiceIdentification and SessionService
//     config live on the resource itself.
//
// SetManagerNic is NOT wired: real set_manager_nic identifies the
// target NIC by string-searching the ENTIRE JSON of each
// EthernetInterface member for the given nic_addr substring (or
// derives one from the connection's own hostname when omitted) — a
// real, genuinely fuzzy-matching quirk this port did not attempt to
// reproduce faithfully without a way to verify it. DeleteVolumes/
// CreateVolume (storage/RAID configuration, genuinely complex
// vendor-varying logic) also stay unwired. All three are absent from
// their category's own command list below — real, disclosed gaps, a
// later increment of this same batch.
//
// Args: category (required); command (required list); baseuri
// (required, real effect); username/password (real effect); auth_token
// (not supported, fails loud); bios_attributes; boot_order;
// secure_boot_enable; power_restore_policy; network_protocols;
// hostinterface_config; hostinterface_id; service_id; sessions_config.
func moduleRedfishConfig(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	commands := argStringList(args, "command")
	if len(commands) == 0 {
		return Result{}, errArg("redfish_config: missing required argument: command")
	}
	if res, ok := redfishCheckCategory("redfish_config", category, redfishConfigCategories); !ok {
		return res, nil
	}
	if res, ok := redfishCheckCommands("redfish_config", category, commands, redfishConfigCategories); !ok {
		return res, nil
	}
	baseuri, username, password, res, ok := redfishtoolCredentials("redfish_config", args)
	if !ok {
		return res, nil
	}
	if res, ok := redfishtoolRequireBinary(ctx, conn, "redfish_config"); !ok {
		return res, nil
	}

	lastChanged := false
	lastMsg := ""
	for _, command := range commands {
		var res Result
		var err error
		switch category {
		case "Systems":
			res, err = redfishConfigSystemsCommand(ctx, conn, baseuri, username, password, command, args)
		case "Manager":
			res, err = redfishConfigManagerCommand(ctx, conn, baseuri, username, password, command, args)
		case "Sessions":
			res, err = redfishConfigSessionsCommand(ctx, conn, baseuri, username, password, command, args)
		}
		if err != nil {
			return Result{}, err
		}
		if res.Failed {
			return res, nil
		}
		lastChanged = res.Changed
		lastMsg = res.Msg
	}

	if lastChanged {
		return Changed(lastMsg), nil
	}
	return Ok(lastMsg), nil
}

var redfishConfigCategories = map[string][]string{
	"Systems": {
		"SetBiosDefaultSettings", "SetBiosAttributes", "SetBootOrder",
		"SetDefaultBootOrder", "EnableSecureBoot", "SetSecureBoot",
		"SetPowerRestorePolicy",
	},
	"Manager": {
		"SetNetworkProtocols", "SetHostInterface", "SetServiceIdentification",
	},
	"Sessions": {"SetSessionService"},
}

func redfishConfigSystemsCommand(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, command string, args map[string]any) (Result, error) {
	switch command {
	case "SetBiosDefaultSettings":
		return redfishSetBiosDefaultSettings(ctx, conn, baseuri, username, password)
	case "SetBiosAttributes":
		return redfishSetBiosAttributes(ctx, conn, baseuri, username, password, args)
	case "SetBootOrder":
		return redfishSetBootOrder(ctx, conn, baseuri, username, password, args)
	case "SetDefaultBootOrder":
		return redfishSetDefaultBootOrder(ctx, conn, baseuri, username, password)
	case "EnableSecureBoot":
		return redfishSetSecureBoot(ctx, conn, baseuri, username, password, true)
	case "SetSecureBoot":
		return redfishSetSecureBoot(ctx, conn, baseuri, username, password, argBool(args, "secure_boot_enable", true))
	case "SetPowerRestorePolicy":
		return redfishSetPowerRestorePolicy(ctx, conn, baseuri, username, password, args)
	}
	return Fail("redfish_config: Systems: unsupported command " + command), nil
}

// redfishSystemsSubResourceURI GETs the bare Systems resource (via the
// same `redfishtool Systems` idiom redfish_command.go's own reset/
// setBootOverride commands already use) and returns the @odata.id of
// one of its own linked sub-resources (e.g. "Bios" or "SecureBoot").
// redfishResourceSubURI GETs a bare top-level resource (via redfishtool's
// own named subcommand — "Systems" or "Managers", both already used
// elsewhere in this batch) and returns the @odata.id of one of its own
// linked sub-resources (e.g. "Bios", "SecureBoot", "NetworkProtocol").
func redfishResourceSubURI(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, topCommand, key string) (string, Result, error) {
	var res map[string]json.RawMessage
	// "-1" (--One) is required: a bare topCommand with no operation
	// argument and no ID-selecting option defaults to "collection" in
	// redfishtool's own SystemsMain/ManagersMain (the len(args)<2
	// branch), not "get" — confirmed from their own source. Without it
	// this would decode a {Members:[...]} collection into the
	// single-resource shape this function expects.
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &res, "-1", topCommand)
	if err != nil {
		return "", Result{}, err
	}
	if r.RC != 0 {
		return "", Fail("redfish_config: " + redfishtoolErrMsg(r)), nil
	}
	raw, ok := res[key]
	if !ok {
		return "", Fail("redfish_config: " + key + " resource not found"), nil
	}
	var link struct {
		ODataID string `json:"@odata.id"`
	}
	if err := json.Unmarshal(raw, &link); err != nil {
		return "", Result{}, err
	}
	if link.ODataID == "" {
		return "", Fail("redfish_config: " + key + " resource not found"), nil
	}
	return link.ODataID, Result{}, nil
}

func redfishSetBiosDefaultSettings(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (Result, error) {
	biosURI, res, err := redfishResourceSubURI(ctx, conn, baseuri, username, password, "Systems", "Bios")
	if err != nil {
		return Result{}, err
	}
	if res.Failed {
		return res, nil
	}
	var bios redfishActionsHolder
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &bios, "raw", "GET", biosURI)
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetBiosDefaultSettings: " + redfishtoolErrMsg(r)), nil
	}
	action, ok := bios.Actions["#Bios.ResetBios"]
	if !ok || action.Target == "" {
		return Fail("redfish_config: SetBiosDefaultSettings: ResetBios action not found"), nil
	}
	pr, err := redfishtoolRun(ctx, conn, baseuri, username, password, "-d", "{}", "raw", "POST", action.Target)
	if err != nil {
		return Result{}, err
	}
	if pr.RC != 0 {
		return Fail("redfish_config: SetBiosDefaultSettings: " + redfishtoolErrMsg(pr)), nil
	}
	return Changed("BIOS set to default settings"), nil
}

func redfishSetBiosAttributes(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, args map[string]any) (Result, error) {
	attrs, _ := args["bios_attributes"].(map[string]any)
	if len(attrs) == 0 {
		return Fail("redfish_config: SetBiosAttributes: must provide bios_attributes"), nil
	}
	biosURI, res, err := redfishResourceSubURI(ctx, conn, baseuri, username, password, "Systems", "Bios")
	if err != nil {
		return Result{}, err
	}
	if res.Failed {
		return res, nil
	}

	var bios struct {
		Attributes map[string]any `json:"Attributes"`
		Settings   struct {
			SettingsObject struct {
				ODataID string `json:"@odata.id"`
			} `json:"SettingsObject"`
		} `json:"@Redfish.Settings"`
	}
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &bios, "raw", "GET", biosURI)
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetBiosAttributes: " + redfishtoolErrMsg(r)), nil
	}

	toPatch := map[string]any{}
	for k, v := range attrs {
		cur, ok := bios.Attributes[k]
		if !ok {
			continue
		}
		if !reflect.DeepEqual(cur, v) {
			toPatch[k] = v
		}
	}
	if len(toPatch) == 0 {
		return Ok("BIOS attributes already set"), nil
	}

	settingsURI := bios.Settings.SettingsObject.ODataID
	if settingsURI == "" {
		return Fail("redfish_config: SetBiosAttributes: settings resource for BIOS attributes not found"), nil
	}
	body, err := json.Marshal(map[string]any{"Attributes": toPatch})
	if err != nil {
		return Result{}, err
	}
	pr, err := redfishtoolRun(ctx, conn, baseuri, username, password, "-d", string(body), "raw", "PATCH", settingsURI)
	if err != nil {
		return Result{}, err
	}
	if pr.RC != 0 {
		return Fail("redfish_config: SetBiosAttributes: " + redfishtoolErrMsg(pr)), nil
	}
	return Changed("Modified BIOS attributes"), nil
}

func redfishSetBootOrder(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, args map[string]any) (Result, error) {
	bootOrder := argStringList(args, "boot_order")
	if len(bootOrder) == 0 {
		return Fail("redfish_config: SetBootOrder: boot_order list required"), nil
	}
	body, err := json.Marshal(map[string]any{"Boot": map[string]any{"BootOrder": bootOrder}})
	if err != nil {
		return Result{}, err
	}
	r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "Systems", "patch", string(body))
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetBootOrder: " + redfishtoolErrMsg(r)), nil
	}
	return Changed("Modified the boot order"), nil
}

func redfishSetDefaultBootOrder(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (Result, error) {
	var sys redfishActionsHolder
	// "-1" (--One): see redfishResourceSubURI's own doc comment — a bare
	// "Systems" call with no ID-selecting option defaults to
	// redfishtool's own "collection" operation, not "get".
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &sys, "-1", "Systems")
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetDefaultBootOrder: " + redfishtoolErrMsg(r)), nil
	}
	action, ok := sys.Actions["#ComputerSystem.SetDefaultBootOrder"]
	if !ok || action.Target == "" {
		return Fail("redfish_config: SetDefaultBootOrder: Action #ComputerSystem.SetDefaultBootOrder not found"), nil
	}
	pr, err := redfishtoolRun(ctx, conn, baseuri, username, password, "-d", "{}", "raw", "POST", action.Target)
	if err != nil {
		return Result{}, err
	}
	if pr.RC != 0 {
		return Fail("redfish_config: SetDefaultBootOrder: " + redfishtoolErrMsg(pr)), nil
	}
	return Changed("BootOrder set to default"), nil
}

func redfishSetSecureBoot(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, enable bool) (Result, error) {
	secureBootURI, res, err := redfishResourceSubURI(ctx, conn, baseuri, username, password, "Systems", "SecureBoot")
	if err != nil {
		return Result{}, err
	}
	if res.Failed {
		return res, nil
	}
	body, err := json.Marshal(map[string]any{"SecureBootEnable": enable})
	if err != nil {
		return Result{}, err
	}
	pr, err := redfishtoolRun(ctx, conn, baseuri, username, password, "-d", string(body), "raw", "PATCH", secureBootURI)
	if err != nil {
		return Result{}, err
	}
	if pr.RC != 0 {
		return Fail("redfish_config: SetSecureBoot: " + redfishtoolErrMsg(pr)), nil
	}
	return Changed("Modified SecureBootEnable"), nil
}

func redfishSetPowerRestorePolicy(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, args map[string]any) (Result, error) {
	policy := argString(args, "power_restore_policy", "")
	if policy == "" {
		return Fail("redfish_config: SetPowerRestorePolicy: must provide power_restore_policy"), nil
	}
	body, err := json.Marshal(map[string]any{"PowerRestorePolicy": policy})
	if err != nil {
		return Result{}, err
	}
	r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "Systems", "patch", string(body))
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetPowerRestorePolicy: " + redfishtoolErrMsg(r)), nil
	}
	return Changed("Modified PowerRestorePolicy"), nil
}

func redfishConfigManagerCommand(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, command string, args map[string]any) (Result, error) {
	switch command {
	case "SetNetworkProtocols":
		return redfishSetNetworkProtocols(ctx, conn, baseuri, username, password, args)
	case "SetHostInterface":
		return redfishSetHostInterface(ctx, conn, baseuri, username, password, args)
	case "SetServiceIdentification":
		return redfishSetServiceIdentification(ctx, conn, baseuri, username, password, args)
	}
	return Fail("redfish_config: Manager: unsupported command " + command), nil
}

func redfishConfigSessionsCommand(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, command string, args map[string]any) (Result, error) {
	if command != "SetSessionService" {
		return Fail("redfish_config: Sessions: unsupported command " + command), nil
	}
	sessionsConfig, _ := args["sessions_config"].(map[string]any)
	if len(sessionsConfig) == 0 {
		return Fail("redfish_config: SetSessionService: must provide sessions_config"), nil
	}
	body, err := json.Marshal(sessionsConfig)
	if err != nil {
		return Result{}, err
	}
	r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "SessionService", "patch", string(body))
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetSessionService: " + redfishtoolErrMsg(r)), nil
	}
	return Changed("Modified session service"), nil
}

func redfishSetServiceIdentification(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, args map[string]any) (Result, error) {
	serviceID := argString(args, "service_id", "")
	if serviceID == "" {
		return Fail("redfish_config: SetServiceIdentification: must provide service_id"), nil
	}
	body, err := json.Marshal(map[string]any{"ServiceIdentification": serviceID})
	if err != nil {
		return Result{}, err
	}
	r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "Managers", "patch", string(body))
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetServiceIdentification: " + redfishtoolErrMsg(r)), nil
	}
	return Changed(""), nil
}

// redfishProtocolBool coerces one of real set_network_protocols' own
// accepted ProtocolEnabled spellings (true/"true"/"True"/"on"/1 and
// their false equivalents) to a real bool — confirmed from its own
// protocol_state_onlist/protocol_state_offlist source, not guessed.
func redfishProtocolBool(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch v {
		case "true", "True", "on":
			return true, true
		case "false", "False", "off":
			return false, true
		}
	case int:
		if v == 1 {
			return true, true
		}
		if v == 0 {
			return false, true
		}
	case float64:
		if v == 1 {
			return true, true
		}
		if v == 0 {
			return false, true
		}
	}
	return false, false
}

var redfishNetworkProtocolServices = map[string]bool{
	"SNMP": true, "VirtualMedia": true, "Telnet": true, "SSDP": true, "IPMI": true,
	"SSH": true, "KVMIP": true, "NTP": true, "HTTP": true, "HTTPS": true,
	"DHCP": true, "DHCPv6": true, "RDP": true, "RFB": true,
}

func redfishNormalizeNetworkProtocols(services map[string]any) (map[string]any, Result) {
	payload := map[string]any{}
	for serviceName, rawProps := range services {
		if !redfishNetworkProtocolServices[serviceName] {
			return nil, Fail("redfish_config: SetNetworkProtocols: service name " + serviceName + " is invalid")
		}
		props, _ := rawProps.(map[string]any)
		out := map[string]any{}
		for propName, value := range props {
			switch propName {
			case "ProtocolEnabled", "protocolenabled":
				b, ok := redfishProtocolBool(value)
				if !ok {
					return nil, Fail("redfish_config: SetNetworkProtocols: value of property " + propName + " is invalid")
				}
				out["ProtocolEnabled"] = b
			case "port", "Port":
				switch v := value.(type) {
				case int:
					out["Port"] = v
				case float64:
					out["Port"] = int(v)
				default:
					return nil, Fail("redfish_config: SetNetworkProtocols: value of property " + propName + " is invalid")
				}
			default:
				out[propName] = value
			}
		}
		payload[serviceName] = out
	}
	return payload, Result{}
}

func redfishSetNetworkProtocols(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, args map[string]any) (Result, error) {
	services, _ := args["network_protocols"].(map[string]any)
	if len(services) == 0 {
		return Fail("redfish_config: SetNetworkProtocols: must provide network_protocols"), nil
	}
	payload, res := redfishNormalizeNetworkProtocols(services)
	if res.Failed {
		return res, nil
	}

	networkProtocolURI, res, err := redfishResourceSubURI(ctx, conn, baseuri, username, password, "Managers", "NetworkProtocol")
	if err != nil {
		return Result{}, err
	}
	if res.Failed {
		return res, nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "-d", string(body), "raw", "PATCH", networkProtocolURI)
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetNetworkProtocols: " + redfishtoolErrMsg(r)), nil
	}
	return Changed("Modified manager network protocol settings"), nil
}

func redfishSetHostInterface(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, args map[string]any) (Result, error) {
	hostinterfaceConfig, _ := args["hostinterface_config"].(map[string]any)
	if len(hostinterfaceConfig) == 0 {
		return Fail("redfish_config: SetHostInterface: must provide hostinterface_config"), nil
	}

	hostInterfacesURI, res, err := redfishResourceSubURI(ctx, conn, baseuri, username, password, "Managers", "HostInterfaces")
	if err != nil {
		return Result{}, err
	}
	if res.Failed {
		return res, nil
	}

	var coll struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &coll, "raw", "GET", hostInterfacesURI)
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_config: SetHostInterface: " + redfishtoolErrMsg(r)), nil
	}

	hostinterfaceID := argString(args, "hostinterface_id", "")
	var target string
	if hostinterfaceID != "" {
		for _, m := range coll.Members {
			trimmed := strings.TrimSuffix(m.ODataID, "/")
			last := trimmed[strings.LastIndex(trimmed, "/")+1:]
			if strings.Contains(last, hostinterfaceID) {
				target = m.ODataID
				break
			}
		}
		if target == "" {
			return Fail("redfish_config: SetHostInterface: HostInterface ID " + hostinterfaceID + " not present"), nil
		}
	} else if len(coll.Members) == 1 {
		target = coll.Members[0].ODataID
	} else {
		return Fail("redfish_config: SetHostInterface: hostinterface_id not defined and multiple interfaces detected"), nil
	}

	body, err := json.Marshal(hostinterfaceConfig)
	if err != nil {
		return Result{}, err
	}
	pr, err := redfishtoolRun(ctx, conn, baseuri, username, password, "-d", string(body), "raw", "PATCH", target)
	if err != nil {
		return Result{}, err
	}
	if pr.RC != 0 {
		return Fail("redfish_config: SetHostInterface: " + redfishtoolErrMsg(pr)), nil
	}
	return Changed("Modified host interface"), nil
}
