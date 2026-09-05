package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the three ilo_redfish_*.go modules share:
// shelling out to `ilorest`, HPE's own official, open-source "RESTful
// Interface Tool" (iLOrest — HPE-published on GitHub/PyPI, covering iLO
// 4/5/6) instead of speaking Redfish HTTPS directly the way real
// ilo_redfish_command.py/ilo_redfish_config.py/ilo_redfish_info.py do
// via module_utils/_redfish_utils.py/_ilo_redfish_utils.py.
//
// # `ilorest` really is a genuine, strong-fit Redfish client — unlike
// # racadm/OneCLI, it has real GENERIC raw-Redfish passthrough commands
//
// Verified against HPE's own iLOrest User Guide (its "Raw commands"
// page, read before writing this file): `rawget <path>` performs a raw
// HTTP GET; `rawpatch <file>`/`rawpost <file>` perform a raw PATCH/POST
// whose body comes from a JSON file shaped `{"path": "<uri>", "body":
// {...}}` (single-resource form) or a `{"<uri>": {...}, ...}` map
// (multi-resource form) — NOT an inline command-line argument. This
// port always uses the single-resource form: it writes that JSON to a
// temp file on the target (via conn.TempPath + a shell redirect, this
// package's own established way to hand a generated file to a command
// that only accepts a file path — see redis.go's own doc comment for the
// same "everything travels through one Exec command string" constraint)
// and passes that file's path to rawpatch/rawpost.
//
// This genuine raw-GET/PATCH/POST capability is why ilo_redfish_info's
// own GetiLOSessions and ilo_redfish_config's own five Set* commands can
// all be reproduced faithfully here (see each module's own file) — a
// meaningfully different, stronger position than idrac_redfish_*'s own
// racadm substitution (racadm has no raw-Redfish-URI equivalent at all)
// or xcc_redfish_command's own OneCLI substitution (same gap, see that
// file's own doc comment).
//
// # Local, in-band ilorest — see redfish_common.go
//
// This port always invokes LOCAL ilorest (never `--url`/`-u`/`-p`) — see
// redfish_common.go's own doc comment for why (this exact reasoning
// applies identically to racadm in idrac_common.go and OneCli in
// xcc_redfish_command.go).
func iloRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	return redfishRequireBinary(ctx, conn, moduleName, "ilorest",
		"this port shells out to HPE's own local ilorest (RESTful Interface Tool) CLI rather than speaking "+
			"Redfish HTTPS directly — see ilo_common.go's own doc comment and redfish_common.go's own doc "+
			"comment on this batch's local/in-band CLI architecture")
}

// iloRawGet runs `ilorest rawget <path>` and, on success, JSON-decodes
// its stdout into out.
func iloRawGet(ctx context.Context, conn remoteexec.Connection, path string, out any) (remoteexec.Result, error) {
	res, err := runStatus(ctx, conn, "ilorest rawget "+shellQuote(path))
	if err != nil {
		return res, err
	}
	if res.RC == 0 && out != nil && strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), out); jerr != nil {
			return res, fmt.Errorf("decoding ilorest rawget %s output: %w", path, jerr)
		}
	}
	return res, nil
}

// iloRawWrite runs `ilorest rawpatch <file>` (or rawpost, per verb) for
// one {path, body} write, staging the JSON body in a temp file on the
// target first — see ilo_common.go's own doc comment on why rawpatch/
// rawpost need a file rather than an inline argument.
func iloRawWrite(ctx context.Context, conn remoteexec.Connection, verb, path string, body any) (remoteexec.Result, error) {
	payload := map[string]any{"path": path, "body": body}
	b, err := json.Marshal(payload)
	if err != nil {
		return remoteexec.Result{}, err
	}
	tmp := conn.TempPath("ilorest-" + verb + ".json")
	cmd := "printf '%s' " + shellQuote(string(b)) + " > " + shellQuote(tmp) +
		" && ilorest " + verb + " " + shellQuote(tmp) + "; rm -f " + shellQuote(tmp)
	return runStatus(ctx, conn, cmd)
}

func iloRawPatch(ctx context.Context, conn remoteexec.Connection, path string, body any) (remoteexec.Result, error) {
	return iloRawWrite(ctx, conn, "rawpatch", path, body)
}

func iloErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
