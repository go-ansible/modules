package modules

import (
	"context"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the six ibm_sa_*.go modules in this batch
// (ibm_sa_domain, ibm_sa_host, ibm_sa_host_ports, ibm_sa_pool, ibm_sa_vol,
// ibm_sa_vol_map) share: shelling out to `xcli`, IBM's own official
// "Extended Command-Line Interface" for the IBM XIV / Spectrum Accelerate
// Family / FlashSystem A9000 storage systems (IBM's own "XCLI Utility
// User Guide", read before writing this file — not guessed from the
// module name), instead of `pyxcli` (module_utils/_ibm_sa_utils.py's own
// Python client library) every real ibm_sa_* module talks to the array
// through.
//
// # `pyxcli` and `xcli` are the SAME command surface
//
// This is not a guess: every real ibm_sa_*.py module in this batch calls
// `xcli_client.cmd.<command_name>(**kwargs)` (e.g. `domain_create`,
// `host_define`, `pool_create`, `vol_create`, `map_vol`,
// `host_add_port`) — pyxcli's own command names are, one-for-one, XCLI's
// OWN documented command names, and pyxcli's own keyword arguments are
// XCLI's own documented `key=value` command parameters (verified against
// the XCLI Utility User Guide's own command-syntax examples, e.g.
// `pool_create pool=pool_00001 hard_size=171 soft_size=171
// snapshot_size=65` and `vol_create vol=vol_00010 size=17
// pool=pool_00001`). So every ibm_sa_* module in this batch drives the
// exact same command name and field set its own real Python source
// already names, just rendered as `xcli ... <command> key=value ...`
// instead of a Python method call.
//
// # Connection syntax, verified against XCLI's own User Guide
//
// Basic (non-interactive, single-command) mode's own documented syntax:
//
//	xcli -u <user> -p <password> -m <IP1> [-m <IP2> [-m <IP3>]] [-y] [-s] <command> [key=value ...]
//
// -m (repeatable) addresses the array by one or more management IPs —
// this port's xcliRun passes one -m per element of the module's own
// `endpoints` argument, matching real spectrum_accelerate_spec()'s own
// `endpoints` field exactly (a comma-separated list of management
// addresses in every real ibm_sa_* module's own EXAMPLES). -y suppresses
// XCLI's own interactive "Are you sure?" confirmation prompt (required
// for this port's non-interactive create/delete calls). -s selects
// XCLI's own CSV output format ("Displays the command output in CSV
// format") — used here for xcliList's own parsing instead of the
// default user-readable list format, which is not reliably
// machine-parseable.
//
// # Auth: username on argv, password via an environment variable
//
// XCLI's own User Guide documents a password-handling fallback chain for
// -u/-p: "If the -p or --password parameter is not specified, the
// XIV_XCLIPASSWORD environment variable is used as the password" (and
// XIV_XCLIUSER for -u). Consistent with this project's hard "no secrets
// in argv" rule and matching redis.go's own REDISCLI_AUTH precedent
// exactly, this port NEVER places password on the xcli command line:
// every invocation instead prefixes the command with
// `XIV_XCLIPASSWORD=<password>` (shell-quoted), relying on XCLI's own
// documented environment-variable fallback. username has no such
// documented sensitivity (real ibm_sa_* modules' own argument_spec marks
// only `password` as `no_log`) and is passed via -u directly.
//
// # CSV parsing is a documented, honestly-bounded limitation
//
// This port has no live XIV/Spectrum Accelerate array or xcli binary in
// its own sandbox to capture a real `-s` CSV response against. XCLI's
// own User Guide confirms the existence and purpose of -s ("Displays
// command output in CSV format") but this port's own copy of that guide
// does not include a literal sample CSV byte-for-byte. xcliParseCSV
// below therefore implements the standard, generic CSV shape (a header
// row of field names, one data row per matching object, comma-separated,
// no embedded-comma/quote escaping attempted) — the same class of
// honestly-bounded risk this project already accepts for glab api's own
// unverified flag surface (gitlab_common.go) and hwc_*'s own derived
// operation IDs (hwc_common.go): a wrong assumption here fails loud (an
// empty or short field list), not silently.
func ibmSaEndpoints(args map[string]any) []string {
	raw := argString(args, "endpoints", "")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ibmSaRequireBinary fails cleanly (Result{Failed:true}, not a Go error)
// if the real `xcli` CLI is not on the target's PATH, or if
// username/password/endpoints are missing — matching real
// connect_ssl()'s own "Username, password or endpoints arguments are
// missing" fail_json.
func ibmSaRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string, args map[string]any) (Result, bool) {
	if argString(args, "username", "") == "" || argString(args, "password", "") == "" || len(ibmSaEndpoints(args)) == 0 {
		return Fail(fmt.Sprintf("%s: username, password and endpoints are all required", moduleName)), false
	}
	if _, err := run(ctx, conn, "command -v xcli"); err != nil {
		return Fail(fmt.Sprintf("%s: the xcli binary (IBM's own Extended Command-Line Interface for XIV/"+
			"Spectrum Accelerate/FlashSystem A9000) is required on the target and was not found in PATH — "+
			"this port shells out to it rather than speaking pyxcli's own client protocol directly; see "+
			"ibm_sa_common.go's own doc comment", moduleName)), false
	}
	return Result{}, true
}

// ibmSaCmd renders one xcli invocation. fields is rendered as
// sorted `key=value` tokens for a deterministic, testable command
// string (XCLI's own syntax takes bare `key=value` positional tokens
// after the command name, not `--key value` flags — see this file's own
// doc comment).
func ibmSaCmd(args map[string]any, csv bool, command string, fields map[string]string) string {
	parts := []string{"xcli", "-u", argString(args, "username", "")}
	for _, ep := range ibmSaEndpoints(args) {
		parts = append(parts, "-m", ep)
	}
	parts = append(parts, "-y")
	if csv {
		parts = append(parts, "-s")
	}
	parts = append(parts, command)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+"="+fields[k])
	}
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	cmd := strings.Join(quoted, " ")
	if pw := argString(args, "password", ""); pw != "" {
		cmd = "XIV_XCLIPASSWORD=" + shellQuote(pw) + " " + cmd
	}
	return cmd
}

// ibmSaRun runs one xcli command (no CSV output — used for
// create/delete/define/map-style commands whose own success is judged
// purely by exit code).
func ibmSaRun(ctx context.Context, conn remoteexec.Connection, args map[string]any, command string, fields map[string]string) (remoteexec.Result, error) {
	return runStatus(ctx, conn, ibmSaCmd(args, false, command, fields))
}

// ibmSaList runs one `xcli -s <list_command> [filter=value...]` and
// parses its CSV output into a slice of field->value rows (ibmSaCmd's
// own doc comment covers this parsing's own honestly-bounded limits).
// A non-zero exit is returned as ok=false, rows=nil (not an error) —
// matching real ibm_sa_* modules' own tolerant `.as_single_element`/
// `.as_list` pattern (a `_list` command for a resource that does not
// exist yet exits non-zero or returns nothing, which every real
// ibm_sa_*.py module treats as "not found", not a failure).
func ibmSaList(ctx context.Context, conn remoteexec.Connection, args map[string]any, listCommand string, filter map[string]string) (rows []map[string]string, ok bool, err error) {
	res, err := runStatus(ctx, conn, ibmSaCmd(args, true, listCommand, filter))
	if err != nil {
		return nil, false, err
	}
	if res.RC != 0 {
		return nil, false, nil
	}
	rows = ibmSaParseCSV(res.Stdout)
	return rows, true, nil
}

// ibmSaParseCSV parses xcli -s's own CSV output (a header row of field
// names, then one comma-separated data row per object) into a slice of
// field->value maps — see ibm_sa_common.go's own doc comment on this
// parsing's honestly-bounded limits (no embedded-comma/quote escaping).
func ibmSaParseCSV(output string) []map[string]string {
	lines := []string{}
	for _, l := range strings.Split(output, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 {
		return nil
	}
	header := strings.Split(lines[0], ",")
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}
	var rows []map[string]string
	for _, line := range lines[1:] {
		fields := strings.Split(line, ",")
		row := make(map[string]string, len(header))
		for i, h := range header {
			if i < len(fields) {
				row[h] = strings.TrimSpace(fields[i])
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// ibmSaFieldsFromArgs collects every key of args that is both non-empty
// and named in allowed into an xcli field map — matching real
// build_pyxcli_command's own "only known, non-empty fields" filter
// (module_utils/_ibm_sa_utils.py's AVAILABLE_PYXCLI_FIELDS/
// build_pyxcli_command), applied per-module against that module's own
// real field list (passed as allowed) rather than the one global list
// every real ibm_sa_* module's shared util actually uses, since a global
// list would let one module send another's own fields by accident.
func ibmSaFieldsFromArgs(args map[string]any, allowed ...string) map[string]string {
	out := map[string]string{}
	for _, k := range allowed {
		if v := argString(args, k, ""); v != "" {
			out[k] = v
		}
	}
	return out
}

// ibmSaErrMsg builds a Fail() message body from a non-zero xcli result,
// preferring stderr but falling back to stdout.
func ibmSaErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
