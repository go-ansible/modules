package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaGetkeytab implements Ansible's `ipa_getkeytab` module:
// manages a Kerberos keytab FILE ON THE TARGET by shelling out to the
// real `ipa-getkeytab` utility (part of freeipa-client, see
// https://manpages.ubuntu.com/manpages/jammy/man1/ipa-getkeytab.1.html)
// — NOT the `ipa` CLI ipa_common.go's other helpers shell out to: real
// ipa_getkeytab's own source (plugins/modules/ipa_getkeytab.py) confirms
// it drives `ipa-getkeytab` directly via its own CmdRunner, never going
// through JSON-RPC or the `ipa` CLI at all, so this module does not use
// ipaRequireBinary/ipaShow/ipaRun (those are for the `ipa` CLI
// specifically) — it has its own, much smaller, "require ipa-getkeytab
// on PATH" check below.
//
// Because it shells out to a plain CLI tool rather than the `ipa` CLI,
// ipa_getkeytab has none of the other ipa_* modules' Kerberos-ticket
// precondition in the same way — `ipa-getkeytab` authenticates over
// LDAP itself (GSSAPI by default, or bind_dn/bind_pw, or sasl_mech), so
// a valid Kerberos ticket is still normally needed unless bind_dn/
// bind_pw is given, but this port does not attempt to distinguish those
// cases; a plain "command failed" is surfaced via Fail() either way.
//
// Args (verified directly from real ipa_getkeytab's own CmdRunner
// arg_formats — the exact source of truth for its CLI flag mapping,
// stronger evidence than the "--<api-param>" convention ipa_common.go's
// other modules rely on, since this IS the real module's own generated
// command line, not an inference from JSON-RPC parameter names):
//   - path (required, aliased keytab) — positional-like via --keytab on
//     `ipa-getkeytab`, and ALSO the target's own filesystem path this
//     port checks with pathExists (real ipa_getkeytab checks
//     os.path.exists(path) locally on the managed host — this port's
//     Connection.Exec-based `test -e` probe is the direct portable
//     equivalent).
//   - principal (required) -> --principal
//   - state (present|absent, default "present") — present only checks
//     for the keytab file's existence (matching real ipa_getkeytab's own
//     documented "present only checks for existence of a file" — if you
//     want to recreate an existing keytab you must set force=true);
//     absent removes the file if present, unconditionally (no
//     `ipa-getkeytab` invocation for absent — matching real
//     ipa_getkeytab, which just os.remove()s the local file).
//   - force (bool) — when the file already exists AND state=present,
//     removes it first and re-runs `ipa-getkeytab` (real ipa_getkeytab's
//     own force semantics exactly: without force, an existing file at
//     path is left untouched and reported unchanged, even if principal
//     or any other arg differs from what's actually in it — this port
//     cannot inspect an existing keytab's contents any more than real
//     ipa_getkeytab can, so this is not a deviation, it's the real
//     module's own behavior).
//   - ipa_host -> --server; ldap_uri -> --ldapuri; bind_dn -> --binddn;
//     bind_pw -> --bindpw; password -> --password; ca_cert -> --cacert;
//     sasl_mech -> --mech; encryption_types -> --enctypes;
//     retrieve_mode (bool) -> --retrieve (present only when true,
//     matching real ipa_getkeytab's own cmd_runner_fmt.as_bool, which
//     omits the flag entirely for a false/absent value).
//
// Deviation vs real ipa_getkeytab: real ipa_getkeytab validates
// ipa_host/ldap_uri as mutually exclusive and retrieve_mode/password as
// mutually exclusive via AnsibleModule's own mutually_exclusive
// argument-spec feature; this port checks the same two pairs by hand in
// Go for an equivalent errArg before ever invoking ipa-getkeytab.
func moduleIpaGetkeytab(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path := argString(args, "path", argString(args, "keytab", ""))
	if path == "" {
		return Result{}, errArg("ipa_getkeytab: path (or keytab) is required")
	}
	principal := argString(args, "principal", "")
	if principal == "" {
		return Result{}, errArg("ipa_getkeytab: principal is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_getkeytab: state must be present or absent, got %q", state)
	}
	if _, hasHost := args["ipa_host"]; hasHost {
		if _, hasURI := args["ldap_uri"]; hasURI {
			return Result{}, errArg("ipa_getkeytab: ipa_host and ldap_uri are mutually exclusive")
		}
	}
	retrieveMode := argBool(args, "retrieve_mode", false)
	if _, hasRM := args["retrieve_mode"]; hasRM && retrieveMode {
		if _, hasPw := args["password"]; hasPw {
			return Result{}, errArg("ipa_getkeytab: retrieve_mode and password are mutually exclusive")
		}
	}
	force := argBool(args, "force", false)

	if _, err := run(ctx, conn, "command -v ipa-getkeytab"); err != nil {
		return Fail("ipa_getkeytab: the ipa-getkeytab binary (from freeipa-client) is required on the target " +
			"and was not found in PATH"), nil
	}

	exists, err := pathExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(path + " already absent"), nil
		}
		if err := conn.Remove(ctx, path); err != nil {
			return Fail("ipa_getkeytab: removing " + path + ": " + err.Error()), nil
		}
		return Changed(path + " removed"), nil
	}

	if exists && !force {
		return Ok(path + " already exists"), nil
	}

	if exists && force {
		if err := conn.Remove(ctx, path); err != nil {
			return Fail("ipa_getkeytab: removing " + path + " before recreation: " + err.Error()), nil
		}
	}

	cmdParts := []string{"ipa-getkeytab", "--keytab=" + path, "--principal=" + principal}
	if retrieveMode {
		cmdParts = append(cmdParts, "--retrieve")
	}
	for _, spec := range ipaGetkeytabFlagSpecs {
		if v := argString(args, spec.arg, ""); v != "" {
			cmdParts = append(cmdParts, "--"+spec.flag+"="+v)
		}
	}

	quoted := make([]string, len(cmdParts))
	for i, p := range cmdParts {
		quoted[i] = shellQuote(p)
	}
	res, err := runStatus(ctx, conn, strings.Join(quoted, " "))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return ipaFailedf("ipa_getkeytab", "ipa-getkeytab", res), nil
	}
	return Changed(path + " created"), nil
}

var ipaGetkeytabFlagSpecs = []struct{ arg, flag string }{
	{"ipa_host", "server"},
	{"ldap_uri", "ldapuri"},
	{"bind_dn", "binddn"},
	{"bind_pw", "bindpw"},
	{"password", "password"},
	{"ca_cert", "cacert"},
	{"sasl_mech", "mech"},
	{"encryption_types", "enctypes"},
}
