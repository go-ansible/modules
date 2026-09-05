package modules

import (
	"context"
	"fmt"
	"sort"

	remoteexec "github.com/go-remoteexec/transport"
)

var idracRedfishConfigCategories = map[string][]string{
	"Manager": {"SetManagerAttributes", "SetLifecycleControllerAttributes", "SetSystemAttributes"},
}

// moduleIdracRedfishConfig implements Ansible's `idrac_redfish_config`
// module: sets one or more iDRAC/Lifecycle-Controller/System attributes.
//
// Args: category (required, must be "Manager"); command (required list,
// each one of SetManagerAttributes/SetLifecycleControllerAttributes/
// SetSystemAttributes — mutually exclusive with each other, matching
// real idrac_redfish_config.py's own CATEGORY_COMMANDS_MUTUALLY_EXCLUSIVE
// check); manager_attributes (dict, default {}) — attribute name/value
// pairs to set; baseuri/username/password/auth_token/timeout/
// resource_id accepted for shape compatibility, NO EFFECT (see
// redfish_common.go's own doc comment).
//
// # racadm mapping
//
// See idrac_common.go's own doc comment for why all three commands map
// onto the SAME `racadm set idrac.<key> <value>` invocation, one per
// manager_attributes entry — real idrac_redfish_config's own manager_
// attributes keys are already spelled in racadm's own dotted
// `<Group>.<Index>.<Object>` shape (its own EXAMPLES use
// "NTPConfigGroup.1.NTPEnable", "SysLog.1.SysLogEnable", etc.), so no
// key translation is needed.
//
// # Idempotency: a documented, bounded simplification
//
// Real set_manager_attributes() first GETs the target Attributes
// resource, skips any requested key already equal to its current value,
// and reports "No changes made. Manager attributes already set." when
// nothing was left to patch. racadm has no equivalently cheap
// "read every requested attribute's current value, structured, in one
// call" operation this port could drive with confidence without a live
// array to verify racadm get's own per-attribute output shape against
// (see redfish_common.go's own doc comment on why this batch avoids
// unverified guesses). This port therefore always issues `racadm set`
// for every entry in manager_attributes unconditionally: Changed=true
// whenever manager_attributes is non-empty (an attribute racadm reports
// as unchanged still costs nothing beyond the extra command, and racadm
// itself is not consulted for a stale echo of "changed" either way,
// matching this project's own precedent of an honestly-bounded
// simplification over a guessed diff — see hwc_common.go's own "create
// is idempotent, update is a no-op" doc comment for the same stance
// applied elsewhere in this port), Ok/unchanged only when
// manager_attributes is empty, matching real idrac_redfish_config's own
// message for that case exactly ("No changes made. Manager attributes
// already set."). A `racadm set` against an attribute name racadm does
// not recognize fails loud (non-zero exit), surfacing as this module's
// own Fail() — this port does not pre-validate attribute names the way
// real set_manager_attributes()'s own `attrs_bad` check does.
func moduleIdracRedfishConfig(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	commands := argStringList(args, "command")
	if len(commands) == 0 {
		return Result{}, errArg("idrac_redfish_config: missing required argument: command")
	}
	if res, ok := redfishCheckCategory("idrac_redfish_config", category, idracRedfishConfigCategories); !ok {
		return res, nil
	}
	if res, ok := redfishCheckCommands("idrac_redfish_config", category, commands, idracRedfishConfigCategories); !ok {
		return res, nil
	}
	if len(commands) > 1 {
		sorted := append([]string(nil), commands...)
		sort.Strings(sorted)
		return Fail(fmt.Sprintf("idrac_redfish_config: parameters are mutually exclusive: %s", formatPyList(sorted))), nil
	}
	if res, ok := racadmRequireBinary(ctx, conn, "idrac_redfish_config"); !ok {
		return res, nil
	}

	attrsRaw, _ := args["manager_attributes"].(map[string]any)
	if len(attrsRaw) == 0 {
		return Ok("No changes made. Manager attributes already set."), nil
	}

	keys := make([]string, 0, len(attrsRaw))
	for k := range attrsRaw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	patched := map[string]any{}
	for _, key := range keys {
		value := fmt.Sprint(attrsRaw[key])
		res, err := racadmSet(ctx, conn, key, value)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(fmt.Sprintf("idrac_redfish_config: %s: %s", commands[0], racadmErrMsg(res))), nil
		}
		patched[key] = value
	}
	return Changed(fmt.Sprintf("%s: Modified Manager attributes %v", commands[0], patched)), nil
}
