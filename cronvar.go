package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCronvar implements Ansible's `cronvar` (community.general)
// module: manages one `NAME=value` environment-variable line in a
// crontab, alongside cron.go's own job-entry management of the same
// crontab. Unlike cron.go's marker-comment scheme, a cronvar entry is
// found by its variable NAME alone — no marker comment is written or
// looked for (verified from the real module's own doc/examples, which
// show a plain "EMAIL=doug@ansibmod.con.com" line with nothing else
// around it).
//
// Args: name (string, required) — the variable's name; value (string,
// required unless state=absent); state (present|absent, default
// "present"); user (string, default "root" — real cronvar's own doc
// states this default explicitly, unlike cron.go's sibling module,
// which instead defaults to the connection's own user; this port
// matches cronvar's documented default rather than cron.go's, and
// always passes `-u <user>` to `crontab`, even when user is left at its
// default); cron_file (string, optional) — manage a file's variable
// line instead of a crontab: without a leading "/", resolved under
// "/etc/cron.d/"; with one, used as-is; insertafter / insertbefore
// (string, optional, present-only) — the name of an existing variable
// to insert the new one immediately after/before, when it doesn't
// already exist; with neither given (or no match for the one given),
// a new variable is appended at the end; backup (bool, default false)
// — before any change, copies the crontab/file's current content to a
// timestamped path under /tmp on the target and returns it in Extra
// under "backup".
//
// Simplifications vs real cronvar: check_mode/diff_mode are not
// meaningful here (real cronvar's own doc marks both unsupported, so
// there is nothing this port is narrowing); insertafter/insertbefore
// only consider other VARIABLE lines (lines matching `NAME=...`) as
// anchor candidates, never a job line — a real crontab commonly
// interleaves variable and job lines, and this port does not attempt
// to also anchor relative to a job line, since cronvar's own doc
// examples only ever anchor relative to another variable.
func moduleCronvar(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("cronvar: state must be present or absent, got %q", state)
	}
	var value string
	if state == "present" {
		value, err = requireString(args, "value")
		if err != nil {
			return Result{}, errArg("cronvar: value is required when state is present")
		}
	}
	user := argString(args, "user", "root")
	cronFile := argString(args, "cron_file", "")
	insertafter := argString(args, "insertafter", "")
	insertbefore := argString(args, "insertbefore", "")
	backup := argBool(args, "backup", false)

	var lines []string
	var readCmd, writeCmd string
	var filePath string

	if cronFile != "" {
		filePath = cronFile
		if !strings.HasPrefix(filePath, "/") {
			filePath = "/etc/cron.d/" + filePath
		}
		res, err := runStatus(ctx, conn, "cat "+shellQuote(filePath)+" 2>/dev/null")
		if err != nil {
			return Result{}, err
		}
		if res.RC == 0 {
			lines = splitLines(res.Stdout)
		}
	} else {
		readCmd = "crontab -u " + shellQuote(user) + " -l"
		writeCmd = "crontab -u " + shellQuote(user) + " -"
		res, err := conn.Exec(ctx, readCmd+" 2>/dev/null", nil)
		if err != nil {
			return Result{}, err
		}
		if res.RC == 0 {
			lines = splitLines(res.Stdout)
		}
	}

	newLines, changed := cronvarApplyEntry(lines, name, value, state, insertafter, insertbefore)
	if !changed {
		return Ok(name + " unchanged"), nil
	}

	result := Changed(name)
	if backup {
		backupPath, err := cronvarBackup(ctx, conn, strings.Join(lines, "\n"), name)
		if err != nil {
			return Result{}, err
		}
		result = result.WithExtra("backup", backupPath)
	}

	content := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		content += "\n"
	}
	if cronFile != "" {
		if err := writeRemote(ctx, conn, filePath, []byte(content)); err != nil {
			return Result{}, err
		}
	} else {
		writeRes, err := conn.Exec(ctx, writeCmd, strings.NewReader(content))
		if err != nil {
			return Result{}, err
		}
		if writeRes.RC != 0 {
			return Fail(fmt.Sprintf("cronvar: crontab: %s", strings.TrimSpace(writeRes.Stderr))), nil
		}
	}
	return result, nil
}

// cronvarVarName returns line's variable name if it looks like a
// "NAME=..." line, and ok=true.
func cronvarVarName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	name, _, ok := strings.Cut(trimmed, "=")
	if !ok || name == "" {
		return "", false
	}
	for _, r := range name {
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return "", false
		}
	}
	return name, true
}

func cronvarApplyEntry(lines []string, name, value, state, insertafter, insertbefore string) ([]string, bool) {
	idx := -1
	for i, l := range lines {
		if n, ok := cronvarVarName(l); ok && n == name {
			idx = i
			break
		}
	}

	if state == "absent" {
		if idx < 0 {
			return lines, false
		}
		out := append([]string{}, lines[:idx]...)
		out = append(out, lines[idx+1:]...)
		return out, true
	}

	desired := name + "=" + value
	if idx >= 0 {
		if lines[idx] == desired {
			return lines, false
		}
		out := append([]string{}, lines...)
		out[idx] = desired
		return out, true
	}

	insertAt := len(lines)
	switch {
	case insertafter != "":
		for i, l := range lines {
			if n, ok := cronvarVarName(l); ok && n == insertafter {
				insertAt = i + 1
				break
			}
		}
	case insertbefore != "":
		for i, l := range lines {
			if n, ok := cronvarVarName(l); ok && n == insertbefore {
				insertAt = i
				break
			}
		}
	}
	out := append([]string{}, lines[:insertAt]...)
	out = append(out, desired)
	out = append(out, lines[insertAt:]...)
	return out, true
}

// cronvarBackup writes content to a timestamped path under /tmp on the
// target, returning that path.
func cronvarBackup(ctx context.Context, conn remoteexec.Connection, content, label string) (string, error) {
	ts, err := run(ctx, conn, "date +%Y%m%d%H%M%S")
	if err != nil {
		return "", err
	}
	path := "/tmp/cronvar-backup-" + label + "-" + ts
	if content != "" {
		content += "\n"
	}
	if _, err := conn.Exec(ctx, "cat > "+shellQuote(path), strings.NewReader(content)); err != nil {
		return "", err
	}
	return path, nil
}
