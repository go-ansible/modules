package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMount implements (a subset of) Ansible's `mount` module:
// manages one entry in `/etc/fstab` (or another file given via
// `fstab`) and, for the mounted/unmounted/remounted states, the live
// mount too. Where mount_facts.go READS current mounts, this module
// WRITES fstab entries and issues `mount`/`umount`.
//
// Args: path (string, required — real mount aliases this from `name`;
// see acl.go/known_hosts.go for this port's standing convention of only
// accepting the canonical name); src, fstype (required for state
// present/mounted); opts (default "defaults"); dump, passno (default
// "0" each); fstab (string, default "/etc/fstab"); backup (bool,
// default false) — copies fstab to a timestamped sibling before
// rewriting it; boot (bool, default true) — false adds "noauto" to
// opts if not already present; state (present|mounted|unmounted|
// absent|absent_from_fstab|remounted, — see below for ephemeral).
//
// State semantics: present/absent_from_fstab/absent/mounted/unmounted/
// remounted are implemented matching real mount's own documented
// behavior (absent also unmounts if currently mounted; mounted creates
// the mount point directory and mounts if not already; unmounted only
// unmounts, fstab untouched; remounted always reports changed, per real
// mount's own documented "This will always return changed=true").
// ephemeral (mount without ever touching fstab, with real mount's own
// safety check that refuses to override a mount point already carrying
// a DIFFERENT source) is NOT implemented — it fails cleanly with a
// clear message rather than silently behaving like `mounted` (which
// would touch fstab, the one thing ephemeral promises not to do).
//
// Simplifications vs real mount: always reads/writes fstab as
// whitespace-separated fields with NO support for the fstab \040
// space-escaping convention (a path containing a literal space is not
// supported), no Solaris/vfstab or BSD-specific format handling
// (mount_facts.go's read side handles BSD `mount` output, but this
// module's fstab WRITE side always uses the Linux/POSIX six-column
// fstab format), and `opts_no_log` is accepted but has no effect
// (there is no argument-logging layer in this port to suppress).
func moduleMount(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	fstabPath := argString(args, "fstab", "/etc/fstab")
	backup := argBool(args, "backup", false)

	switch state {
	case "present", "mounted":
		src, err := requireString(args, "src")
		if err != nil {
			return Result{}, errArg("mount: src is required when state is %s", state)
		}
		fstype, err := requireString(args, "fstype")
		if err != nil {
			return Result{}, errArg("mount: fstype is required when state is %s", state)
		}
		opts := argString(args, "opts", "defaults")
		if !argBool(args, "boot", true) && !hasMountOpt(opts, "noauto") {
			opts = opts + ",noauto"
		}
		dump := argString(args, "dump", "0")
		passno := argString(args, "passno", "0")
		desired := strings.Join([]string{src, path, fstype, opts, dump, passno}, " ")

		fstabChanged, err := writeFstabEntry(ctx, conn, fstabPath, path, desired, backup)
		if err != nil {
			return Result{}, err
		}

		if state == "present" {
			if fstabChanged {
				return Changed(path + " added to " + fstabPath), nil
			}
			return Ok(path + " already in " + fstabPath), nil
		}

		// state == "mounted"
		if _, err := run(ctx, conn, "mkdir -p "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		mounted, err := isMountPoint(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if mounted {
			if fstabChanged {
				return Changed(path + " fstab entry updated"), nil
			}
			return Ok(path + " already mounted"), nil
		}
		if _, err := run(ctx, conn, "mount "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		return Changed(path + " mounted"), nil

	case "unmounted":
		mounted, err := isMountPoint(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if !mounted {
			return Ok(path + " already unmounted"), nil
		}
		if _, err := run(ctx, conn, "umount "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		return Changed(path + " unmounted"), nil

	case "absent", "absent_from_fstab":
		fstabChanged, err := removeFstabEntry(ctx, conn, fstabPath, path, backup)
		if err != nil {
			return Result{}, err
		}
		changed := fstabChanged
		if state == "absent" {
			mounted, err := isMountPoint(ctx, conn, path)
			if err != nil {
				return Result{}, err
			}
			if mounted {
				if _, err := run(ctx, conn, "umount "+shellQuote(path)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}
		if changed {
			return Changed(path + " removed"), nil
		}
		return Ok(path + " already absent"), nil

	case "remounted":
		cmd := "mount -o remount"
		if opts := argString(args, "opts", ""); opts != "" {
			cmd = "mount -o remount," + opts
		}
		cmd += " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(path + " remounted"), nil

	case "ephemeral":
		return Fail("mount: state=ephemeral is not implemented in this port (see moduleMount's doc comment); " +
			"use state=mounted, which does touch fstab, or issue `mount` directly via the command/shell module"), nil

	default:
		return Result{}, errArg("mount: unknown state %q", state)
	}
}

func hasMountOpt(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == want {
			return true
		}
	}
	return false
}

// isMountPoint reports whether path is currently a mount point, by
// reusing mount_facts.go's own gatherMounts (which tries /proc/mounts,
// then falls back to parsing plain `mount` output for BSD/macOS
// targets) — the same source-priority this port already established
// for reading current mount state.
func isMountPoint(ctx context.Context, conn remoteexec.Connection, path string) (bool, error) {
	mounts, err := gatherMounts(ctx, conn)
	if err != nil {
		return false, err
	}
	for _, m := range mounts {
		if m["mount_point"] == path {
			return true, nil
		}
	}
	return false, nil
}

// readFstabLines reads path's raw lines, tolerating a missing file
// (returned as no lines, not an error) — fstab management can
// legitimately run against a fresh file this module itself is about to
// create the first entry in.
func readFstabLines(ctx context.Context, conn remoteexec.Connection, path string) ([]string, error) {
	res, err := runStatus(ctx, conn, "cat "+shellQuote(path)+" 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	return splitLines(res.Stdout), nil
}

// fstabEntryIndex returns the index of the line whose second
// whitespace-separated field (the mount point) equals path, or -1.
func fstabEntryIndex(lines []string, path string) int {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && fields[1] == path {
			return i
		}
	}
	return -1
}

// writeFstabEntry ensures fstabPath contains desired as the entry for
// path (replacing an existing entry with a different exact line, or
// appending one), returning whether it changed anything.
func writeFstabEntry(ctx context.Context, conn remoteexec.Connection, fstabPath, path, desired string, backup bool) (bool, error) {
	lines, err := readFstabLines(ctx, conn, fstabPath)
	if err != nil {
		return false, err
	}
	idx := fstabEntryIndex(lines, path)
	if idx >= 0 {
		if lines[idx] == desired {
			return false, nil
		}
		lines[idx] = desired
	} else {
		lines = append(lines, desired)
	}
	if err := writeFstabLines(ctx, conn, fstabPath, lines, backup); err != nil {
		return false, err
	}
	return true, nil
}

// removeFstabEntry removes path's entry from fstabPath, if present.
func removeFstabEntry(ctx context.Context, conn remoteexec.Connection, fstabPath, path string, backup bool) (bool, error) {
	lines, err := readFstabLines(ctx, conn, fstabPath)
	if err != nil {
		return false, err
	}
	idx := fstabEntryIndex(lines, path)
	if idx < 0 {
		return false, nil
	}
	lines = append(lines[:idx], lines[idx+1:]...)
	if err := writeFstabLines(ctx, conn, fstabPath, lines, backup); err != nil {
		return false, err
	}
	return true, nil
}

func writeFstabLines(ctx context.Context, conn remoteexec.Connection, fstabPath string, lines []string, backup bool) error {
	if backup {
		if _, err := run(ctx, conn, "cp "+shellQuote(fstabPath)+" "+shellQuote(fstabPath)+".$(date +%Y%m%d%H%M%S) 2>/dev/null"); err != nil {
			return err
		}
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	_, err := conn.Exec(ctx, "cat > "+shellQuote(fstabPath), strings.NewReader(content))
	return err
}
