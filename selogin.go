package modules

import (
	"context"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSelogin implements Ansible's `selogin` module: manages the
// mapping from a Linux user (or `%group`) to an SELinux user, similar
// to `semanage login`.
//
// Args: login (string, required) — a Linux user, `%group`, or the
// special `__default__` entry; seuser (string, required when
// state=present, matching real selogin's own required_if) — the
// SELinux user name; selevel (string, aliased serange, default "s0") —
// MLS/MCS range; state (present|absent, default "present"); reload
// (bool, default true); ignore_selinux_state (bool, default false,
// accepted as a no-op — see sefcontext.go's identical note).
//
// Real selogin is implemented entirely against the Python `seobject`
// binding (seobject.loginRecords), never a CLI. This port composes the
// `semanage login` command instead, the tool seobject itself wraps:
// `-a`/`-m` to add or modify (add when login is not yet mapped, modify
// when it is mapped to a different seuser/selevel — mirroring real
// selogin's own add-vs-modify branch), `-d` to delete, `-s` for seuser,
// `-r` for selevel, `-N` to suppress the post-commit reload.
//
// Idempotency is checked via `semanage login -l`, parsed one row per
// line as "<login>  <seuser>  <range>  <service>" (columns separated by
// runs of two or more spaces, real semanage's own heading row is
// tolerated by simply never matching a real login name). Like this
// batch's other `semanage`-based modules, this exact column shape is
// this port's own assumption, not verified against a live SELinux
// system in this sandbox — a disclosed limitation.
func moduleSelogin(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	login, err := requireString(args, "login")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("selogin: state must be present or absent, got %q", state)
	}
	seuser := argString(args, "seuser", "")
	if state == "present" && seuser == "" {
		return Result{}, errArg("selogin: seuser is required when state is present")
	}
	selevel := argString(args, "selevel", argString(args, "serange", "s0"))
	reload := argBool(args, "reload", true)

	listOut, err := run(ctx, conn, "semanage login -l")
	if err != nil {
		return Result{}, err
	}
	rows := parseSeloginList(listOut)

	changed := false
	switch state {
	case "present":
		row, found := rows[login]
		switch {
		case !found:
			if _, err := run(ctx, conn, seloginCmd("a", login, seuser, selevel, reload)); err != nil {
				return Result{}, err
			}
			changed = true
		case row.seuser != seuser || row.selevel != selevel:
			if _, err := run(ctx, conn, seloginCmd("m", login, seuser, selevel, reload)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	case "absent":
		if _, found := rows[login]; found {
			if _, err := run(ctx, conn, seloginCmd("d", login, "", "", reload)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	res := Result{Changed: changed}
	res = res.WithExtra("login", login).WithExtra("seuser", seuser).
		WithExtra("serange", selevel).WithExtra("state", state)
	return res, nil
}

type seloginRow struct {
	seuser  string
	selevel string
}

var seloginCols = regexp.MustCompile(`\s{2,}`)

func parseSeloginList(out string) map[string]seloginRow {
	rows := map[string]seloginRow{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), "\r")
		if line == "" {
			continue
		}
		fields := seloginCols.Split(line, -1)
		if len(fields) < 3 {
			continue
		}
		if fields[0] == "Login Name" { // semanage's own heading row
			continue
		}
		rows[fields[0]] = seloginRow{seuser: fields[1], selevel: fields[2]}
	}
	return rows
}

func seloginCmd(action, login, seuser, selevel string, reload bool) string {
	var b strings.Builder
	b.WriteString("semanage login -")
	b.WriteString(action)
	if seuser != "" {
		b.WriteString(" -s ")
		b.WriteString(shellQuote(seuser))
	}
	if selevel != "" {
		b.WriteString(" -r ")
		b.WriteString(shellQuote(selevel))
	}
	if !reload {
		b.WriteString(" -N")
	}
	b.WriteString(" ")
	b.WriteString(shellQuote(login))
	return b.String()
}
