package modules

import (
	"context"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSefcontext implements Ansible's `sefcontext` module: manages
// SELinux file-context mapping definitions, similar to `semanage
// fcontext`.
//
// Args: target (string, required; aliased from path) — the target path
// expression; ftype (a|b|c|d|f|l|p|s, default "a") — the file type the
// mapping applies to (all/block/char/directory/regular/symlink/
// pipe/socket, matching real sefcontext's own single-letter choices);
// setype (string) — the SELinux type for target; seuser (string) —
// SELinux user for target, left to semanage's own default ("system_u"
// for a new mapping) when unset; selevel (string, aliased serange) —
// SELinux range, left to semanage's own default ("s0" for a new
// mapping) when unset; substitute (string, aliased equal) — makes
// target's context equivalent to this path instead of giving it its own
// setype; state (present|absent, default "present"); reload (bool,
// default true) — reload the policy after commit; ignore_selinux_state
// (bool, default false) — accepted, a no-op (this port has no runtime
// SELinux-enabled probe of its own to skip, the same note seboolean.go
// makes about its own identical argument).
//
// setype and substitute are mutually exclusive; state=present requires
// exactly one of them (matching real sefcontext's own documented
// constraint). state=absent with NEITHER given removes both a type
// mapping and an equivalence mapping for target, if either exists —
// also matching real sefcontext's documented behavior.
//
// Real sefcontext is implemented entirely against the Python `seobject`
// binding (seobject.fcontextRecords), never a CLI. This port has no
// such binding, only shell access, so it composes the `semanage
// fcontext` command instead — the same tool seobject wraps: `-a`/`-m`/
// `-d` to add/modify/delete, `-f` for ftype, `-t` for setype, `-s` for
// seuser, `-r` for selevel, `-e` for substitute (equivalence), `-N` to
// suppress the reload real sefcontext's own `reload=false` performs via
// the binding instead of a flag.
//
// Idempotency is checked by fetching `semanage fcontext -C -l` (customized
// mappings only, matching seobject's own get_all() semantics of listing
// what has actually been added locally rather than the compiled-in
// policy defaults) ONCE and parsing it in Go, the same "fetch, then
// parse locally" shape as this package's other listing-based modules
// (e.g. firewalld_info.go's `firewall-cmd --list-all` parse). This
// port's parse assumes two possible row shapes it has not been able to
// verify against a live SELinux system in this sandbox — a disclosed
// limitation shared with this batch's other `semanage`-based modules:
//   - a setype mapping row: "<target>  <ftype display>  <seuser>:object_r:<setype>:<selevel>",
//     columns separated by runs of two or more spaces;
//   - an equivalence row: "<target> = <substitute>".
//
// real sefcontext does NOT apply the new context to any existing file
// (only future relabeling / an explicit `restorecon` does), and neither
// does this port — matching real sefcontext's own documented behavior,
// not a gap.
func moduleSefcontext(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	target := argString(args, "target", argString(args, "path", ""))
	if target == "" {
		return Result{}, errArg("sefcontext: target (or its alias path) is required")
	}
	ftype := argString(args, "ftype", "a")
	switch ftype {
	case "a", "b", "c", "d", "f", "l", "p", "s":
	default:
		return Result{}, errArg("sefcontext: ftype must be one of a, b, c, d, f, l, p, s, got %q", ftype)
	}
	setype := argString(args, "setype", "")
	seuser := argString(args, "seuser", "")
	selevel := argString(args, "selevel", argString(args, "serange", ""))
	substitute := argString(args, "substitute", argString(args, "equal", ""))
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("sefcontext: state must be present or absent, got %q", state)
	}
	if setype != "" && substitute != "" {
		return Result{}, errArg("sefcontext: setype and substitute are mutually exclusive")
	}
	if state == "present" && setype == "" && substitute == "" {
		return Result{}, errArg("sefcontext: one of setype or substitute is required when state is present")
	}
	reload := argBool(args, "reload", true)

	listOut, err := run(ctx, conn, "semanage fcontext -C -l")
	if err != nil {
		return Result{}, err
	}
	typeRows, equivRows := parseSefcontextList(listOut)

	changed := false
	switch state {
	case "present":
		if substitute != "" {
			cur, found := equivRows[target]
			if !found {
				if _, err := run(ctx, conn, sefcontextEquivCmd("a", target, substitute, reload)); err != nil {
					return Result{}, err
				}
				changed = true
			} else if cur != substitute {
				if _, err := run(ctx, conn, sefcontextEquivCmd("m", target, substitute, reload)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		} else {
			row, found := typeRows[sefcontextKey{target, ftype}]
			wantSeuser, wantSelevel := seuser, selevel
			if !found {
				if _, err := run(ctx, conn, sefcontextTypeCmd("a", ftype, setype, seuser, selevel, target, reload)); err != nil {
					return Result{}, err
				}
				changed = true
			} else if row.setype != setype ||
				(wantSeuser != "" && row.seuser != wantSeuser) ||
				(wantSelevel != "" && row.selevel != wantSelevel) {
				if _, err := run(ctx, conn, sefcontextTypeCmd("m", ftype, setype, seuser, selevel, target, reload)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}

	case "absent":
		switch {
		case substitute != "":
			if cur, found := equivRows[target]; found && cur == substitute {
				if _, err := run(ctx, conn, sefcontextEquivCmd("d", target, substitute, reload)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		case setype != "":
			if row, found := typeRows[sefcontextKey{target, ftype}]; found && row.setype == setype {
				if _, err := run(ctx, conn, sefcontextTypeCmd("d", ftype, setype, "", "", target, reload)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		default:
			for k, row := range typeRows {
				if k.target == target {
					if _, err := run(ctx, conn, sefcontextTypeCmd("d", k.ftype, row.setype, "", "", target, reload)); err != nil {
						return Result{}, err
					}
					changed = true
				}
			}
			if sub, found := equivRows[target]; found {
				if _, err := run(ctx, conn, sefcontextEquivCmd("d", target, sub, reload)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}
	}

	res := Result{Changed: changed}
	res = res.WithExtra("target", target).WithExtra("state", state)
	return res, nil
}

type sefcontextKey struct {
	target string
	ftype  string
}

type sefcontextRow struct {
	setype  string
	seuser  string
	selevel string
}

var sefcontextFtypeDisplay = map[string]string{
	"a": "all files",
	"b": "block device",
	"c": "character device",
	"d": "directory",
	"f": "regular file",
	"l": "symbolic link",
	"p": "named pipe",
	"s": "socket",
}

var sefcontextFtypeFromDisplay = func() map[string]string {
	m := map[string]string{}
	for k, v := range sefcontextFtypeDisplay {
		m[v] = k
	}
	return m
}()

var sefcontextCols = regexp.MustCompile(`\s{2,}`)

// parseSefcontextList parses `semanage fcontext -C -l` output into
// setype-keyed rows and target=substitute equivalence rows. See the
// package doc comment on moduleSefcontext for the two row shapes
// assumed here.
func parseSefcontextList(out string) (map[sefcontextKey]sefcontextRow, map[string]string) {
	types := map[sefcontextKey]sefcontextRow{}
	equivs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if t, s, ok := strings.Cut(line, " = "); ok {
			equivs[strings.TrimSpace(t)] = strings.TrimSpace(s)
			continue
		}
		fields := sefcontextCols.Split(strings.TrimSpace(line), -1)
		if len(fields) < 3 {
			continue
		}
		ftype, ok := sefcontextFtypeFromDisplay[fields[1]]
		if !ok {
			continue
		}
		ctxParts := strings.Split(fields[2], ":")
		if len(ctxParts) < 4 {
			continue
		}
		types[sefcontextKey{fields[0], ftype}] = sefcontextRow{
			setype:  ctxParts[2],
			seuser:  ctxParts[0],
			selevel: strings.Join(ctxParts[3:], ":"),
		}
	}
	return types, equivs
}

func sefcontextTypeCmd(action, ftype, setype, seuser, selevel, target string, reload bool) string {
	var b strings.Builder
	b.WriteString("semanage fcontext -")
	b.WriteString(action)
	b.WriteString(" -f ")
	b.WriteString(ftype)
	if setype != "" {
		b.WriteString(" -t ")
		b.WriteString(shellQuote(setype))
	}
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
	b.WriteString(shellQuote(target))
	return b.String()
}

func sefcontextEquivCmd(action, target, substitute string, reload bool) string {
	var b strings.Builder
	b.WriteString("semanage fcontext -")
	b.WriteString(action)
	b.WriteString(" -e ")
	b.WriteString(shellQuote(substitute))
	if !reload {
		b.WriteString(" -N")
	}
	b.WriteString(" ")
	b.WriteString(shellQuote(target))
	return b.String()
}
