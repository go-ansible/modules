package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

var idracRedfishInfoCategories = map[string][]string{
	"Manager": {"GetManagerAttributes"},
}

// moduleIdracRedfishInfo implements Ansible's `idrac_redfish_info`
// module: reads back the iDRAC/Lifecycle-Controller/System attribute
// registry. Real idrac_redfish_info.py implements exactly one command,
// GetManagerAttributes, which returns a list of {Id, Attributes} entries
// — one per Dell attribute registry ("DellAttributes") member exposed
// under the Manager resource (upstream's own real Ids: "iDRACAttributes",
// "LCAttributes", "SystemAttributes").
//
// # racadm mapping, and an honestly-documented shape gap
//
// racadm's own full-configuration export — `racadm get -f <file> -t
// ini` (independently confirmed via WebSearch against Dell's own racadm
// documentation before writing this file) — dumps EVERY attribute group
// on the iDRAC as INI sections (`[<Group>.<Index>]` headers, `Key=Value`
// lines beneath), which is the same underlying attribute registry real
// GetManagerAttributes reads, just not already partitioned into the
// same iDRACAttributes/LCAttributes/SystemAttributes three-way split —
// that split is a Redfish-resource-level grouping this port has no
// live array to verify a reliable INI-group-name-to-Redfish-Id mapping
// against (per this batch's own "don't guess unverified mappings" rule
// — see redfish_common.go's own doc comment). Rather than fabricate that
// three-way split, this port returns the WHOLE flattened registry as a
// single entry ({"Id": "Attributes", "Attributes": {"<Group>.<Index>.
// <Key>": "<value>", ...}}) under `redfish_facts.entries` — the same
// return KEY real GetManagerAttributes uses, holding strictly MORE data
// (every group, not just the three Redfish exposes) in a flatter shape,
// an honestly-documented shape difference rather than a silent partial
// match.
//
// Args: category (required, must be "Manager"); command (required list,
// each must be "GetManagerAttributes"); baseuri/username/password/
// auth_token/timeout accepted for shape compatibility, NO EFFECT (see
// redfish_common.go's own doc comment).
func moduleIdracRedfishInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	commands := argStringList(args, "command")
	if len(commands) == 0 {
		return Result{}, errArg("idrac_redfish_info: missing required argument: command")
	}
	if res, ok := redfishCheckCategory("idrac_redfish_info", category, idracRedfishInfoCategories); !ok {
		return res, nil
	}
	if res, ok := redfishCheckCommands("idrac_redfish_info", category, commands, idracRedfishInfoCategories); !ok {
		return res, nil
	}
	if res, ok := racadmRequireBinary(ctx, conn, "idrac_redfish_info"); !ok {
		return res, nil
	}

	tmp := conn.TempPath("idrac-racadm-get.ini")
	cmd := "racadm get -f " + shellQuote(tmp) + " -t ini && cat " + shellQuote(tmp) + "; rm -f " + shellQuote(tmp)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("idrac_redfish_info: GetManagerAttributes: " + racadmErrMsg(res)), nil
	}

	attrs := racadmParseINI(res.Stdout)
	entries := []map[string]any{{"Id": "Attributes", "Attributes": attrs}}
	return Ok("").WithExtra("redfish_facts", map[string]any{"entries": entries}), nil
}

// racadmParseINI parses racadm's own `get -f <file> -t ini` output (see
// this file's own doc comment) into a flat "<Group.Index>.<Key>" ->
// value map.
func racadmParseINI(output string) map[string]string {
	out := map[string]string{}
	section := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if section != "" {
			key = section + "." + key
		}
		out[key] = val
	}
	return out
}
