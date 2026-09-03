package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleModprobe implements (a subset of) Ansible's `modprobe`
// (community.general) module: loads/unloads a kernel module, and
// optionally makes that persistent across reboots.
//
// Args: name (string, required); state (present|absent, default
// "present"); params (string, default "") — module parameters, e.g.
// "numdummies=2", passed through to `modprobe` as separate shell-quoted
// words; persistent (disabled|absent|present, default "disabled") —
// "present" writes /etc/modules-load.d/<name>.conf (just the module
// name) and, if params is non-empty, /etc/modprobe.d/<name>.conf
// ("options <name> <params>"); "absent" comments out an existing
// uncommented line in each of those files (if present) rather than
// deleting the files; "disabled" (the default) touches neither.
//
// Idempotency for the live load/unload uses `lsmod | grep -qw`, which
// lists modules with underscores regardless of how they were loaded;
// per real modprobe's own doc note that module names can use `-` and
// `_` interchangeably, this port normalizes `name` to underscores
// (modprobeLsmodName) ONLY for that lsmod check — the name passed to
// `modprobe`/`modprobe -r` itself, and to the persistence files, is
// left exactly as given.
//
// Simplifications vs real modprobe: `persistent` is documented upstream
// as "works only with distributions that use systemd"; this port does
// not check for systemd's presence before writing the modules-load.d/
// modprobe.d files, so a non-systemd persistent=present/absent request
// silently writes files nothing will read, rather than being rejected —
// a real gap, but detecting "does this target use systemd" cheaply and
// reliably from shell alone (short of `command -v systemctl`, itself an
// imperfect signal) was judged not worth the complexity for a
// documented, narrow miss. The two persistence files are named
// `<name>.conf` under each directory — real modprobe's own file-naming
// convention was not visible from ansible-doc's output and was not
// independently verified; this is a reasonable, self-consistent choice
// rather than a confirmed match to upstream.
func moduleModprobe(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("modprobe: state must be present or absent, got %q", state)
	}
	params := argString(args, "params", "")
	persistent := argString(args, "persistent", "disabled")
	if persistent != "disabled" && persistent != "absent" && persistent != "present" {
		return Result{}, errArg("modprobe: persistent must be disabled, absent, or present, got %q", persistent)
	}

	loaded, err := modprobeLoaded(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	changed := false
	switch state {
	case "present":
		if !loaded {
			cmd := "modprobe " + shellQuote(name)
			for _, field := range strings.Fields(params) {
				cmd += " " + shellQuote(field)
			}
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true
		}
	case "absent":
		if loaded {
			if _, err := run(ctx, conn, "modprobe -r "+shellQuote(name)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if persistent != "disabled" {
		persistChanged, err := modprobePersistentApply(ctx, conn, name, params, persistent)
		if err != nil {
			return Result{}, err
		}
		changed = changed || persistChanged
	}

	if changed {
		return Changed(name), nil
	}
	return Ok(name + " unchanged"), nil
}

func modprobeLsmodName(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

func modprobeLoaded(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "lsmod | grep -qw "+shellQuote(modprobeLsmodName(name)))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

func modprobePersistentApply(ctx context.Context, conn remoteexec.Connection, name, params, persistent string) (bool, error) {
	loadPath := "/etc/modules-load.d/" + name + ".conf"
	optsPath := "/etc/modprobe.d/" + name + ".conf"
	changed := false

	switch persistent {
	case "present":
		if c, err := modprobeWriteIfDiffer(ctx, conn, loadPath, name+"\n"); err != nil {
			return false, err
		} else {
			changed = changed || c
		}
		if params != "" {
			if c, err := modprobeWriteIfDiffer(ctx, conn, optsPath, "options "+name+" "+params+"\n"); err != nil {
				return false, err
			} else {
				changed = changed || c
			}
		}
	case "absent":
		if c, err := modprobeCommentIfActive(ctx, conn, loadPath, name); err != nil {
			return false, err
		} else {
			changed = changed || c
		}
		if c, err := modprobeCommentIfActive(ctx, conn, optsPath, "options "+name+" "+params); err != nil {
			return false, err
		} else {
			changed = changed || c
		}
	}
	return changed, nil
}

func modprobeWriteIfDiffer(ctx context.Context, conn remoteexec.Connection, path, content string) (bool, error) {
	current, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return false, err
	}
	if current != nil && string(current) == content {
		return false, nil
	}
	if err := writeRemote(ctx, conn, path, []byte(content)); err != nil {
		return false, err
	}
	return true, nil
}

// modprobeCommentIfActive comments out path's first line if it is
// exactly line and not already commented. A missing file, or a file
// whose first line doesn't match, is a no-op.
func modprobeCommentIfActive(ctx context.Context, conn remoteexec.Connection, path, line string) (bool, error) {
	current, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return false, err
	}
	if current == nil {
		return false, nil
	}
	lines := splitLines(string(current))
	if len(lines) == 0 || lines[0] != line {
		return false, nil
	}
	lines[0] = "#" + line
	content := strings.Join(lines, "\n") + "\n"
	if err := writeRemote(ctx, conn, path, []byte(content)); err != nil {
		return false, err
	}
	return true, nil
}
