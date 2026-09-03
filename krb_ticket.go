package modules

import (
	"context"
	"io"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKrbTicket implements Ansible's `krb_ticket` (community.general)
// module: obtains or destroys a Kerberos ticket via the `kinit'/`klist'/
// `kdestroy' base utilities — the same three commands real krb_ticket's
// own module_utils wraps (there is no library form to substitute here,
// unlike redis.go/consul_kv.go in this same batch: real krb_ticket
// already shells out to these binaries itself).
//
// Args: state (default present: present=kinit, absent=kdestroy);
// principal (default: the user running kinit); password — sent to
// `kinit` over stdin, never as a command-line argument (kinit reads an
// interactively-prompted password from its controlling terminal or
// stdin; this port pipes it in the same way homectl.go's own
// moduleHomectl pipes a password to `openssl passwd -stdin`), so it
// never appears in the target's process listing; keytab (bool) +
// keytab_path — `-k` (+ `-t <path>`, or `-i` for the default client
// keytab when keytab_path is unset); at least one of password/
// keytab_path is required when state=present, matching real
// krb_ticket's own required_if; cache_name (`-c`, kinit/kdestroy/used to
// scope the klist presence check); lifetime (`-l`), start_time (`-s`),
// renewable (`-r`) — string values with real kinit's own s/m/h/d
// duration-suffix syntax, passed through unvalidated; forwardable,
// proxiable, address_restricted — tri-state (true/false/unset) booleans
// mapped to kinit's own paired flags (-f/-F, -p/-P, -a/-A respectively;
// unset omits both, matching real krb_ticket's own `ignore_none`
// formatting); anonymous (`-n`), canonicalization (`-C`), enterprise
// (`-E`), renewal (`-R`), validate (`-v`) — plain booleans; kdestroy_all
// (bool) — `kdestroy -A`, destroying every cache in the collection
// regardless of state=absent's own idempotency check.
//
// state=present is idempotent on klist: if no principal/cache_name is
// given, presence is `klist`'s own exit code; otherwise `klist -l`'s
// output is grepped for the principal and/or cache_name substrings —
// matching real krb_ticket's own check_ticket_present exactly. A ticket
// already present is a no-op; otherwise `kinit` runs and Changed is
// reported if it exits zero.
//
// state=absent runs kdestroy (unconditionally if kdestroy_all, otherwise
// only if check_ticket_present is true), matching real krb_ticket's own
// logic.
//
// Deviation from real krb_ticket: real krb_ticket's own check_mode
// support (full: check_ticket_present alone decides Changed without
// ever invoking kinit/kdestroy) is not implemented — this port has no
// check-mode concept at all (see module.go's own doc comment).
func moduleKrbTicket(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("krb_ticket: state must be one of present, absent, got %q", state)
	}
	keytab := argBool(args, "keytab", false)
	keytabPath := argString(args, "keytab_path", "")
	if keytabPath != "" && !keytab {
		return Result{}, errArg("krb_ticket: keytab_path requires keytab=true")
	}
	principal := argString(args, "principal", "")
	cacheName := argString(args, "cache_name", "")
	password := argString(args, "password", "")

	if state == "present" && password == "" && keytabPath == "" {
		return Result{}, errArg("krb_ticket: state=present requires password or keytab_path")
	}

	present, err := krbTicketPresent(ctx, conn, principal, cacheName)
	if err != nil {
		return Result{}, err
	}

	if state == "present" {
		if present {
			return Ok("ticket already present"), nil
		}
		ok, errMsg, err := krbKinit(ctx, conn, args)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Fail("krb_ticket: kinit failed: " + errMsg), nil
		}
		return Changed("obtained Kerberos ticket"), nil
	}

	// state == absent
	if argBool(args, "kdestroy_all", false) {
		ok, errMsg, err := krbKdestroy(ctx, conn, args, true)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Fail("krb_ticket: kdestroy -A failed: " + errMsg), nil
		}
		return Changed("destroyed all Kerberos ticket caches"), nil
	}
	if !present {
		return Ok("no ticket present"), nil
	}
	ok, errMsg, err := krbKdestroy(ctx, conn, args, false)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Fail("krb_ticket: kdestroy failed: " + errMsg), nil
	}
	return Changed("destroyed Kerberos ticket"), nil
}

// krbTicketPresent matches real krb_ticket's own check_ticket_present:
// bare `klist`'s exit code when neither principal nor cache_name is
// given, otherwise a substring search of `klist -l`'s output.
func krbTicketPresent(ctx context.Context, conn remoteexec.Connection, principal, cacheName string) (bool, error) {
	if principal == "" && cacheName == "" {
		res, err := conn.Exec(ctx, "klist", nil)
		if err != nil {
			return false, err
		}
		return res.RC == 0, nil
	}
	res, err := conn.Exec(ctx, "klist -l", nil)
	if err != nil {
		return false, err
	}
	out := res.Stdout
	if principal != "" && !strings.Contains(out, principal) {
		return false, nil
	}
	if cacheName != "" && !strings.Contains(out, cacheName) {
		return false, nil
	}
	return true, nil
}

// krbKinit builds and runs the `kinit` command line for moduleKrbTicket,
// piping password over stdin (see moduleKrbTicket's own doc comment).
// ok is false (with a non-transport errMsg) for a normal kinit failure
// (bad password, unreachable KDC); err is reserved for a Connection
// transport failure.
func krbKinit(ctx context.Context, conn remoteexec.Connection, args map[string]any) (ok bool, errMsg string, err error) {
	var a []string
	if v := argString(args, "lifetime", ""); v != "" {
		a = append(a, "-l", v)
	}
	if v := argString(args, "start_time", ""); v != "" {
		a = append(a, "-s", v)
	}
	if v := argString(args, "renewable", ""); v != "" {
		a = append(a, "-r", v)
	}
	a = append(a, krbTriBoolFlags(args, "forwardable", "-f", "-F")...)
	a = append(a, krbTriBoolFlags(args, "proxiable", "-p", "-P")...)
	a = append(a, krbTriBoolFlags(args, "address_restricted", "-a", "-A")...)
	if argBool(args, "anonymous", false) {
		a = append(a, "-n")
	}
	if argBool(args, "canonicalization", false) {
		a = append(a, "-C")
	}
	if argBool(args, "enterprise", false) {
		a = append(a, "-E")
	}
	if argBool(args, "renewal", false) {
		a = append(a, "-R")
	}
	if argBool(args, "validate", false) {
		a = append(a, "-v")
	}
	keytab := argBool(args, "keytab", false)
	keytabPath := argString(args, "keytab_path", "")
	if keytab {
		a = append(a, "-k")
		if keytabPath != "" {
			a = append(a, "-t", keytabPath)
		} else {
			a = append(a, "-i")
		}
	}
	if cn := argString(args, "cache_name", ""); cn != "" {
		a = append(a, "-c", cn)
	}
	if p := argString(args, "principal", ""); p != "" {
		a = append(a, p)
	}

	quoted := make([]string, len(a))
	for i, v := range a {
		quoted[i] = shellQuote(v)
	}
	cmd := "kinit"
	if len(quoted) > 0 {
		cmd += " " + strings.Join(quoted, " ")
	}

	var stdin io.Reader
	if password := argString(args, "password", ""); password != "" {
		stdin = strings.NewReader(password + "\n")
	}
	res, err := conn.Exec(ctx, cmd, stdin)
	if err != nil {
		return false, "", err
	}
	if res.RC != 0 {
		return false, strings.TrimSpace(res.Stderr), nil
	}
	return true, "", nil
}

// krbTriBoolFlags returns []string{trueFlag} or []string{falseFlag} if
// key is explicitly set in args, or nil if unset — matching real
// krb_ticket's own `ignore_none` formatting for forwardable/proxiable/
// address_restricted, which real kinit itself defaults from krb5.conf
// when neither flag is given.
func krbTriBoolFlags(args map[string]any, key, trueFlag, falseFlag string) []string {
	if _, ok := args[key]; !ok {
		return nil
	}
	if argBool(args, key, false) {
		return []string{trueFlag}
	}
	return []string{falseFlag}
}

// krbKdestroy builds and runs `kdestroy`, with `-A` if all is true,
// otherwise `-c <cache_name>`/`-p <principal>` when given.
func krbKdestroy(ctx context.Context, conn remoteexec.Connection, args map[string]any, all bool) (ok bool, errMsg string, err error) {
	var a []string
	if all {
		a = append(a, "-A")
	}
	if cn := argString(args, "cache_name", ""); cn != "" {
		a = append(a, "-c", cn)
	}
	if p := argString(args, "principal", ""); p != "" {
		a = append(a, "-p", p)
	}
	quoted := make([]string, len(a))
	for i, v := range a {
		quoted[i] = shellQuote(v)
	}
	cmd := "kdestroy"
	if len(quoted) > 0 {
		cmd += " " + strings.Join(quoted, " ")
	}
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return false, "", err
	}
	if res.RC != 0 {
		return false, strings.TrimSpace(res.Stderr), nil
	}
	return true, "", nil
}
