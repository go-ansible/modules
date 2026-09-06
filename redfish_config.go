package modules

import (
	"context"
	"encoding/json"
	"reflect"

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
// DeleteVolumes/CreateVolume (storage/RAID configuration, genuinely
// complex vendor-varying logic) and the whole Manager/Sessions
// categories (SetNetworkProtocols/SetManagerNic/SetHostInterface/
// SetServiceIdentification/SetSessionService) are declared with empty
// command lists — real categories, not wired yet, a later increment of
// this same batch.
//
// Args: category (required); command (required list); baseuri
// (required, real effect); username/password (real effect); auth_token
// (not supported, fails loud); bios_attributes; boot_order;
// secure_boot_enable; power_restore_policy.
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
		res, err := redfishConfigSystemsCommand(ctx, conn, baseuri, username, password, command, args)
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
	"Manager":  {},
	"Sessions": {},
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
func redfishSystemsSubResourceURI(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, key string) (string, Result, error) {
	var sys map[string]json.RawMessage
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &sys, "Systems")
	if err != nil {
		return "", Result{}, err
	}
	if r.RC != 0 {
		return "", Fail("redfish_config: " + redfishtoolErrMsg(r)), nil
	}
	raw, ok := sys[key]
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
	biosURI, res, err := redfishSystemsSubResourceURI(ctx, conn, baseuri, username, password, "Bios")
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
	biosURI, res, err := redfishSystemsSubResourceURI(ctx, conn, baseuri, username, password, "Bios")
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
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &sys, "Systems")
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
	secureBootURI, res, err := redfishSystemsSubResourceURI(ctx, conn, baseuri, username, password, "SecureBoot")
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
