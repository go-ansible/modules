package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOpendjBackendprop implements (a subset of) Ansible's
// `opendj_backendprop` module: ensures one OpenDJ backend configuration
// property has a given value, via the `dsconfig get-backend-prop`/
// `set-backend-prop` subcommands — read from real
// opendj_backendprop.py's own BackendProp class (this batch's hard
// rule: the exact dsconfig flag names and the get-before-set idempotency
// check are only visible in the implementation, not EXAMPLES/OPTIONS).
//
// Args: opendj_bindir (string, default "/opt/opendj/bin"); hostname
// (string, required); port (string, required); username (string,
// default "cn=Directory Manager"); password (string) or passwordfile
// (string, path) — exactly one of the two is required; backend
// (string, required); name (string, required) — the property to set;
// value (string, required); state (string, default "present") —
// accepted for compatibility, but has NO effect here, matching a
// genuine quirk in real opendj_backendprop's own main(): it declares
// `state` in argument_spec but never once reads
// module.params["state"] — real opendj_backendprop always behaves as
// if state=present regardless of what's given. Reproduced faithfully,
// not "fixed", since faking a state=absent behavior the real module
// has never had would be less honest than replicating its own quirk.
//
// Secret handling: real opendj_backendprop puts `password` directly on
// dsconfig's own command line via `-w <password>` when passwordfile is
// not used. This port never does that (this project's own hard rule:
// a secret must never appear in a command line) — a `password` arg is
// instead written to a target-side temp file (same
// conn.TempPath+`cat > file`+`umask 077` pattern ldap_common.go's own
// ldapWriteTempFile uses for LDAP bind passwords) and passed to
// dsconfig via `-j <tempfile>` instead of `-w`, functionally identical
// to what dsconfig's own `-j` (read password from file) flag is for. A
// `passwordfile` arg (already a path, not inline secret text) is
// passed through as `-j <passwordfile>` unchanged, matching real
// behavior exactly since there is no secret-on-a-command-line concern
// there.
//
// Idempotency, including a real, faithfully-reproduced quirk: `dsconfig
// get-backend-prop` output is scanned line by line, each line's first
// two whitespace-separated fields compared against name/value (matching
// real validate_data's own `config_line.split()` + `split_line[0] ==
// name and split_line[1] == value` check) — BUT, matching real main()'s
// own `if stdout and not opendj.validate_data(...)`, if get-backend-prop
// returns NO output at all, this port (like real opendj_backendprop)
// reports unchanged WITHOUT EVER RUNNING set-backend-prop — a real
// upstream quirk (an empty get-backend-prop result presumably means the
// property/backend doesn't exist as far as dsconfig is concerned, and
// real code silently treats that the same as "already correct" rather
// than attempting to set it), reproduced exactly rather than "fixed"
// into more sensible behavior this port's own implementation never had
// a mandate to invent.
func moduleOpendjBackendprop(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	bindir := argString(args, "opendj_bindir", "/opt/opendj/bin")
	hostname, err := requireString(args, "hostname")
	if err != nil {
		return Result{}, err
	}
	port, err := requireString(args, "port")
	if err != nil {
		return Result{}, err
	}
	username := argString(args, "username", "cn=Directory Manager")
	password := argString(args, "password", "")
	passwordfile := argString(args, "passwordfile", "")
	if (password == "") == (passwordfile == "") {
		return Result{}, errArg("opendj_backendprop: exactly one of password or passwordfile is required")
	}
	backend, err := requireString(args, "backend")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	value, err := requireString(args, "value")
	if err != nil {
		return Result{}, err
	}

	dsconfig := bindir + "/dsconfig"

	pwFile := passwordfile
	cleanup := func() {}
	if password != "" {
		var werr error
		pwFile, cleanup, werr = ldapWriteTempFile(ctx, conn, "opendj-backendprop-pw", password)
		if werr != nil {
			return Result{}, werr
		}
	}
	defer cleanup()

	connFlags := "-h " + shellQuote(hostname) + " --port " + shellQuote(port) +
		" --bindDN " + shellQuote(username) + " -j " + shellQuote(pwFile) + " --backend-name " + shellQuote(backend)

	getCmd := dsconfig + " get-backend-prop -n -X -s " + connFlags
	res, err := runStatus(ctx, conn, getCmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("opendj_backendprop: " + strings.TrimSpace(res.Stderr)), nil
	}

	stdout := res.Stdout
	if stdout == "" {
		// Real quirk, faithfully reproduced — see this function's own
		// doc comment.
		return Ok(""), nil
	}
	if opendjBackendpropHasValue(stdout, name, value) {
		return Ok(""), nil
	}

	setCmd := dsconfig + " set-backend-prop -n -X " + connFlags + " --set " + shellQuote(name+":"+value)
	setRes, err := runStatus(ctx, conn, setCmd)
	if err != nil {
		return Result{}, err
	}
	if setRes.RC != 0 {
		return Fail("opendj_backendprop: " + strings.TrimSpace(setRes.Stderr)), nil
	}
	return Changed(""), nil
}

// opendjBackendpropHasValue implements real validate_data: each
// dsconfig output line is whitespace-split, and a line whose first
// field is name and second field is value means the property is
// already correctly set.
func opendjBackendpropHasValue(stdout, name, value string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == name && fields[1] == value {
			return true
		}
	}
	return false
}
