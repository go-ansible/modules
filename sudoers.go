package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSudoers implements (a subset of) Ansible's `sudoers`
// (community.general) module: writes one rule file under
// `/etc/sudoers.d/<name>`, validated with `visudo` before being moved
// into place.
//
// Args: name (string, required) — also the sudoers.d filename; state
// (present|absent, default "present"); user, group (string, mutually
// exclusive — exactly one identifies who the rule grants to); host
// (string, default "ALL"); commands ([]string) — required (together
// with, or instead of, defaults) for state=present, since a rule with
// neither grants nor Defaults directives has nothing to write; runas
// (string, optional) — target user in parens, e.g. "(root)"; nopassword
// (bool, default true) — emits the "NOPASSWD:" tag; setenv, noexec
// (bool, default false each) — emit "SETENV:"/"NOEXEC:"; defaults
// ([]string) — each becomes a "Defaults:<who> <entry>" line before the
// grant line; sudoers_path (string, default "/etc/sudoers.d");
// validation (detect|required|absent, default "detect") — whether/how
// to run `visudo -c -f` on the rendered file before installing it.
//
// The file is rendered on the control node, written to a temp path on
// the target via conn.Put, checked with `visudo -c -f <tmp>` per
// `validation` (detect: only if visudo is found; required: fail if
// visudo isn't found; absent: skip the check entirely), then moved into
// place and chmod'd 0440 (the mode real sudoers.d files are expected to
// carry). A failed validation returns visudo's own stderr in Fail's
// message, and the rendered temp file is left in place for inspection
// rather than silently discarded.
//
// Simplifications vs real sudoers: no backup of a replaced file (unlike
// pam_limits.go/mount.go's `backup` option, real sudoers has none to
// begin with — its idempotency is "the whole file's content", so a
// changed rule is simply overwritten); ownership of the installed file
// is left as whatever `mv`+`chmod` produce (no explicit chown to
// root:root, since sudoers.d files are conventionally only writable by
// a root connection in the first place); `commands: ALL` is passed
// through literally (real sudoers treats `ALL` as a keyword, not a
// path, and this port does not special-case or quote it).
func moduleSudoers(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("sudoers: state must be present or absent, got %q", state)
	}
	sudoersPath := argString(args, "sudoers_path", "/etc/sudoers.d")
	dest := sudoersPath + "/" + name

	if state == "absent" {
		exists, err := pathExists(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(dest + " already absent"), nil
		}
		if _, err := run(ctx, conn, "rm -f "+shellQuote(dest)); err != nil {
			return Result{}, err
		}
		return Changed(dest + " removed"), nil
	}

	user := argString(args, "user", "")
	group := argString(args, "group", "")
	if user != "" && group != "" {
		return Result{}, errArg("sudoers: user and group are mutually exclusive")
	}
	if user == "" && group == "" {
		return Result{}, errArg("sudoers: exactly one of user or group is required")
	}

	content, err := sudoersBuildContent(args, user, group)
	if err != nil {
		return Result{}, err
	}

	current, err := fetchIfExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	if current != nil && string(current) == content {
		return Ok(dest + " unchanged"), nil
	}

	validation := argString(args, "validation", "detect")
	if validation != "detect" && validation != "required" && validation != "absent" {
		return Result{}, errArg("sudoers: validation must be detect, required, or absent, got %q", validation)
	}

	tmpPath := conn.TempPath("sudoers-" + name)
	if err := writeRemote(ctx, conn, tmpPath, []byte(content)); err != nil {
		return Result{}, err
	}

	if validation != "absent" {
		hasVisudo, err := sudoersHasVisudo(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if !hasVisudo {
			if validation == "required" {
				_ = conn.Remove(ctx, tmpPath)
				return Fail("sudoers: validation=required but visudo was not found on the target"), nil
			}
			// validation == "detect" and no visudo: skip silently.
		} else {
			res, err := runStatus(ctx, conn, "visudo -c -f "+shellQuote(tmpPath))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				_ = conn.Remove(ctx, tmpPath)
				return Fail(fmt.Sprintf("sudoers: visudo validation failed: %s", strings.TrimSpace(res.Stderr+res.Stdout))), nil
			}
		}
	}

	if _, err := run(ctx, conn, "mv "+shellQuote(tmpPath)+" "+shellQuote(dest)); err != nil {
		return Result{}, err
	}
	if _, err := run(ctx, conn, "chmod 0440 "+shellQuote(dest)); err != nil {
		return Result{}, err
	}
	return Changed(dest), nil
}

// sudoersBuildContent renders the full sudoers.d file content for a
// present-state rule: any `Defaults:<who> ...` lines, then (if commands
// were given) the grant line itself.
func sudoersBuildContent(args map[string]any, user, group string) (string, error) {
	who := user
	if group != "" {
		who = "%" + group
	}
	host := argString(args, "host", "ALL")
	commands := argStringList(args, "commands")
	defaults := argStringList(args, "defaults")

	if len(commands) == 0 && len(defaults) == 0 {
		return "", errArg("sudoers: at least one of commands or defaults is required for state=present")
	}

	var b strings.Builder
	for _, d := range defaults {
		fmt.Fprintf(&b, "Defaults:%s %s\n", who, d)
	}

	if len(commands) > 0 {
		runas := argString(args, "runas", "")
		var tags []string
		if argBool(args, "nopassword", true) {
			tags = append(tags, "NOPASSWD:")
		}
		if argBool(args, "noexec", false) {
			tags = append(tags, "NOEXEC:")
		}
		if argBool(args, "setenv", false) {
			tags = append(tags, "SETENV:")
		}

		line := who + " " + host + " ="
		if runas != "" {
			line += " (" + runas + ")"
		}
		if len(tags) > 0 {
			line += " " + strings.Join(tags, " ")
		}
		line += " " + strings.Join(commands, ", ")
		b.WriteString(line + "\n")
	}

	return b.String(), nil
}

// sudoersHasVisudo reports whether visudo is available on the target.
func sudoersHasVisudo(ctx context.Context, conn remoteexec.Connection) (bool, error) {
	res, err := runStatus(ctx, conn, "command -v visudo >/dev/null 2>&1")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
