package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOmapiHost implements Ansible's `omapi_host` (community.general)
// module: creates or removes a DHCP host reservation on an ISC DHCPd
// server via the OMAPI protocol.
//
// Architectural note: real omapi_host speaks OMAPI's own binary
// protocol directly, in-process, via the `pypureomapi` Python library —
// no equivalent exists in this port's dependency set. Matching this
// batch's own assignment brief, this port instead drives ISC's own
// `omshell` command-line tool (part of the isc-dhcp-server tooling,
// present anywhere OMAPI itself is meaningful) via `conn`, feeding it
// omshell's own line-oriented scripting language on stdin — the same
// interactive-script-over-stdin shape at.go's own module already uses
// for a different CLI tool.
//
// Args: host (string, default "localhost") — the OMAPI server; port
// (int, default 7911); key_name/key (string, both required) — the TSIG
// key omshell authenticates with; state (present|absent, required);
// hostname (string, alias name, required if state=present) — the lease
// hostname; macaddr (string, required) — the lease's hardware address,
// and the sole lookup key this port matches an existing host by (real
// omapi_host can also be constructed to look up other ways, but
// matching by MAC is its own primary documented path too); ip (string,
// optional); ddns (bool, default false) — matches real omapi_host's own
// behavior of prefixing the OMAPI `statements` attribute with
// `ddns-hostname "<hostname>";`; statements ([]string, default []) —
// additional DHCP statements (without a trailing semicolon, matching
// real omapi_host's own doc), joined with real omapi_host's own "; "
// separator.
//
// State semantics: present looks the host up by macaddr first (an
// omshell `set hardware-address = ...` + `open`); if found, this is a
// no-op (Ok) — unlike real omapi_host, THIS PORT DOES NOT diff and
// update an existing host's fields (real omapi_host does attempt a
// per-field update, but itself also documents that OMAPI never returns
// `statements` back, so even real omapi_host cannot detect a
// statements-only change; see its own code comment to that effect).
// Diffing the rest of omshell's own free-text `open` dump reliably was
// judged not worth the fragility for this batch — a documented,
// deliberate gap, not a silent one. If not found, a `new host` object
// is created with name/hardware-address/hardware-type/ip-address/
// statements as given. absent looks the host up the same way; if found,
// it is opened and removed; if not, this is a no-op.
//
// Because omshell has no machine-readable output mode, "found" vs.
// "not found" is detected with a substring match against omshell's own
// well-known "can't open object: no match" text; this is a documented
// best-effort (this port's sandbox has no real omshell binary to verify
// the exact wording against byte-for-byte), not a structured parse.
// This port also does not populate the `lease` Extra field real
// omapi_host returns (a structured dict from OMAPI's own binary
// response) since omshell's own text output isn't reliably parseable
// into that same shape — documented as not implemented rather than
// faked.
func moduleOmapiHost(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("omapi_host: state must be present or absent, got %q", state)
	}
	keyName, err := requireString(args, "key_name")
	if err != nil {
		return Result{}, err
	}
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	macaddr, err := requireString(args, "macaddr")
	if err != nil {
		return Result{}, err
	}
	host := argString(args, "host", "localhost")
	port := argInt(args, "port", 7911)
	hostname := argString(args, "hostname", argString(args, "name", ""))

	preamble := "server " + host + "\n" +
		"port " + strconv.Itoa(port) + "\n" +
		"key " + keyName + " " + key + "\n" +
		"connect\n" +
		"new host\n"

	lookup := preamble + "set hardware-address = " + macaddr + "\nopen\n"
	res, err := conn.Exec(ctx, "omshell", strings.NewReader(lookup))
	if err != nil {
		return Result{}, err
	}
	found := !strings.Contains(res.Stdout, "no match")

	switch state {
	case "present":
		if hostname == "" {
			return Result{}, errArg("omapi_host: hostname is required when state=present")
		}
		if found {
			return Ok(hostname + " already present"), nil
		}
		script := preamble +
			"set name = \"" + hostname + "\"\n" +
			"set hardware-address = " + macaddr + "\n" +
			"set hardware-type = 1\n"
		if ip := argString(args, "ip", ""); ip != "" {
			script += "set ip-address = " + ip + "\n"
		}
		if stmt := omapiStatements(args, hostname); stmt != "" {
			script += "set statements = \"" + strings.ReplaceAll(stmt, `"`, `\"`) + "\"\n"
		}
		script += "create\n"
		res, err := conn.Exec(ctx, "omshell", strings.NewReader(script))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 || strings.Contains(res.Stdout, "can't create") {
			return Fail("omapi_host: failed to create " + hostname + ": " + strings.TrimSpace(res.Stdout)), nil
		}
		return Changed(hostname + " created"), nil

	default: // absent
		if !found {
			return Ok("host already absent"), nil
		}
		script := preamble + "set hardware-address = " + macaddr + "\nopen\nremove\n"
		res, err := conn.Exec(ctx, "omshell", strings.NewReader(script))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 || strings.Contains(res.Stdout, "can't remove") {
			return Fail("omapi_host: failed to remove host: " + strings.TrimSpace(res.Stdout)), nil
		}
		return Changed("host removed"), nil
	}
}

// omapiStatements builds the OMAPI "statements" attribute value,
// matching real omapi_host's own construction: a `ddns-hostname`
// statement first if ddns is set, then every user-given statement
// joined with "; ".
func omapiStatements(args map[string]any, hostname string) string {
	var parts []string
	if argBool(args, "ddns", false) {
		parts = append(parts, `ddns-hostname "`+hostname+`"`)
	}
	statements := argStringList(args, "statements")
	if len(statements) > 0 {
		parts = append(parts, strings.Join(statements, "; "))
	}
	return strings.Join(parts, "; ")
}
