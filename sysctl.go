package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSysctl implements (a subset of) Ansible's `sysctl` module:
// manages one `key = value` entry in a sysctl config file (default
// "/etc/sysctl.conf") and optionally applies it live.
//
// Args: name (string, required, aliased from key); value (string,
// required unless state=absent, aliased from val); state (present|
// absent, default "present"); sysctl_file (string, default
// "/etc/sysctl.conf"); reload (bool, default true) — runs `sysctl -p
// <sysctl_file>` after a file change; sysctl_set (bool, default false)
// — also verifies/sets the LIVE value via `sysctl -w` (checked first
// via `sysctl -n`, for idempotency); ignoreerrors (bool, default
// false) — adds `-e` to both the `-p` reload and any `-w` set, so an
// unknown key doesn't fail the whole operation (matching real sysctl's
// own documented purpose for this argument).
//
// Unlike cron.go's marker-comment scheme, sysctl.conf uses plain
// "key = value" lines with no marker — an existing entry is found by
// its key alone (the first "=" -delimited field, trimmed), and either
// rewritten in place (preserving its position in the file) or removed;
// a new entry is appended at the end. Simplifications vs real sysctl:
// no `backup` (real sysctl always writes a `.<timestamp>` backup before
// rewriting; this port does not — an intentional narrowing, since
// mount.go already covers this pattern for the one module in this
// batch whose real spec explicitly documents a `backup` option).
func moduleSysctl(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("sysctl: state must be present or absent, got %q", state)
	}
	sysctlFile := argString(args, "sysctl_file", "/etc/sysctl.conf")
	reload := argBool(args, "reload", true)
	sysctlSet := argBool(args, "sysctl_set", false)
	ignoreErrors := argBool(args, "ignoreerrors", false)

	var value string
	if state == "present" {
		value, err = requireString(args, "value")
		if err != nil {
			return Result{}, errArg("sysctl: value is required when state is present")
		}
	}

	res, err := runStatus(ctx, conn, "cat "+shellQuote(sysctlFile)+" 2>/dev/null")
	if err != nil {
		return Result{}, err
	}
	var lines []string
	if res.RC == 0 {
		lines = strings.Split(res.Stdout, "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	}

	newLines, fileChanged := applySysctlEntry(lines, name, value, state)
	if fileChanged {
		content := strings.Join(newLines, "\n")
		if len(newLines) > 0 {
			content += "\n"
		}
		if _, err := conn.Exec(ctx, "cat > "+shellQuote(sysctlFile), strings.NewReader(content)); err != nil {
			return Result{}, err
		}
	}

	liveChanged := false
	if sysctlSet && state == "present" {
		errFlag := ""
		if ignoreErrors {
			errFlag = " -e"
		}
		cur, err := run(ctx, conn, "sysctl"+errFlag+" -n "+shellQuote(name))
		if err != nil {
			return Result{}, err
		}
		if cur != value {
			if _, err := run(ctx, conn, "sysctl"+errFlag+" -w "+shellQuote(name+"="+value)); err != nil {
				return Result{}, err
			}
			liveChanged = true
		}
	}

	if fileChanged && reload {
		errFlag := ""
		if ignoreErrors {
			errFlag = " -e"
		}
		if _, err := run(ctx, conn, "sysctl"+errFlag+" -p "+shellQuote(sysctlFile)); err != nil {
			return Result{}, err
		}
	}

	if fileChanged || liveChanged {
		return Changed(name), nil
	}
	return Ok(name + " unchanged"), nil
}

// applySysctlEntry replaces name's "key = value" line in place (or
// removes it, for state=absent), or appends a new one for state=present
// when no existing line matches. An existing line's key is compared
// after trimming whitespace around it; a commented-out line ("#key=..."
// or "# key = ...") is never treated as a match.
func applySysctlEntry(lines []string, name, value, state string) ([]string, bool) {
	found := false
	changed := false
	out := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, l)
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(key) != name {
			out = append(out, l)
			continue
		}
		found = true
		if state == "absent" {
			changed = true
			continue // drop this line
		}
		newLine := name + " = " + value
		if l != newLine {
			changed = true
		}
		out = append(out, newLine)
	}
	if state == "present" && !found {
		out = append(out, name+" = "+value)
		changed = true
	}
	return out, changed
}
