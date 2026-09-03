package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCrypttab implements (a subset of) Ansible's `crypttab` module:
// manages one encrypted-block-device entry in `/etc/crypttab` — the
// same "one entry, identified by a key field, in a structured
// whitespace-separated config file, read via `cat path` and rewritten
// via `cat > path`" shape as mount.go's own fstab management (and
// sysctl.go's own config-file handling), adapted to crypttab(5)'s four
// columns (name, backing-device, password, options) in place of
// fstab's six.
//
// Args: name (string, required) — the device's name as it appears in
// crypttab; a "/dev/mapper/" prefix is stripped, matching real
// crypttab's own documented behavior; backing_device (string) —
// required when state=present and no entry for name exists yet;
// password (string, default "none" when unset — see below); opts
// (string, comma-delimited, default "none" when unset); path (string,
// default "/etc/crypttab"); state (present|absent|opts_present|
// opts_absent, REQUIRED — real crypttab has no default, unlike this
// port's other config-file modules, and this matches that exactly).
//
// state=present/absent manage the whole entry line, mirroring
// mount.go's writeFstabEntry/removeFstabEntry: present writes the full
// "name device password opts" line, but — unlike mount.go's own
// src/fstype, which it always requires outright — each of
// backing_device/password/opts, when omitted, INHERITS the existing
// entry's current value for that field rather than a fixed default; a
// field only falls back to the literal "none" when there is no existing
// entry to inherit from (a brand-new entry with that field omitted).
// This partial-update behavior is what real crypttab's own "update its
// definition if already present" phrasing describes, and matches its
// own documented example of setting only `opts` on an already-existing
// device without repeating its backing_device. Creating a brand-new
// entry (no existing entry for name) still requires backing_device —
// there is nothing sensible to create a fresh line from otherwise.
// absent removes the entry entirely. state=opts_present/opts_absent
// instead modify only an EXISTING entry's options field in place —
// adding/updating opts by their key (the text before "=", so
// `cipher=aes-cbc-essiv:sha256` replaces any existing `cipher=...`
// regardless of its old value, matching real crypttab's own documented
// "options with different values are updated") or removing opts by that
// same key — and both FAIL if no entry for name exists yet, since
// there's no backing_device to create one from.
//
// password/opts are written as the literal string "none" when a field
// is left empty, matching crypttab(5)'s own convention that "none" (or
// "-") in the password field means "prompt at boot" — used here as
// this port's placeholder for "field intentionally blank" in both the
// password and options columns.
//
// Simplification vs real crypttab: unlike mount.go's own `mount`
// module, real crypttab has no `backup` argument at all, so this port
// doesn't add one either (nothing to deviate from). No mode/owner/
// group/attributes/selinux(se*)/unsafe_writes — crypttab has none of
// those either.
func moduleCrypttab(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	name = strings.TrimPrefix(name, "/dev/mapper/")
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	path := argString(args, "path", "/etc/crypttab")

	lines, err := crypttabReadLines(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	idx := crypttabEntryIndex(lines, name)

	switch state {
	case "present":
		existingDevice, existingPassword, existingOpts := "", "none", "none"
		if idx >= 0 {
			ef := strings.Fields(lines[idx])
			if len(ef) > 1 {
				existingDevice = ef[1]
			}
			if len(ef) > 2 {
				existingPassword = ef[2]
			}
			if len(ef) > 3 {
				existingOpts = ef[3]
			}
		}
		backingDevice := argString(args, "backing_device", "")
		if backingDevice == "" {
			if idx < 0 {
				return Result{}, errArg("crypttab: backing_device is required to create a new entry for %q", name)
			}
			backingDevice = existingDevice
		}
		password := argString(args, "password", "")
		if password == "" {
			password = existingPassword
		}
		opts := argString(args, "opts", "")
		if opts == "" {
			opts = existingOpts
		}
		desired := strings.Join([]string{name, backingDevice, password, opts}, " ")
		if idx >= 0 {
			if lines[idx] == desired {
				return Ok(name + " already up to date in " + path), nil
			}
			lines[idx] = desired
		} else {
			lines = append(lines, desired)
		}
		if err := crypttabWriteLines(ctx, conn, path, lines); err != nil {
			return Result{}, err
		}
		return Changed(name + " updated in " + path), nil

	case "absent":
		if idx < 0 {
			return Ok(name + " already absent from " + path), nil
		}
		lines = append(lines[:idx], lines[idx+1:]...)
		if err := crypttabWriteLines(ctx, conn, path, lines); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed from " + path), nil

	case "opts_present", "opts_absent":
		if idx < 0 {
			return Fail(name + " has no existing entry in " + path + " to modify options on"), nil
		}
		fields := strings.Fields(lines[idx])
		for len(fields) < 4 {
			fields = append(fields, "none")
		}
		existingOpts := crypttabSplitOpts(fields[3])
		wantOpts := crypttabSplitOpts(argString(args, "opts", ""))
		var newOpts []string
		if state == "opts_present" {
			newOpts = crypttabMergeOpts(existingOpts, wantOpts)
		} else {
			newOpts = crypttabRemoveOpts(existingOpts, wantOpts)
		}
		optsField := strings.Join(newOpts, ",")
		if optsField == "" {
			optsField = "none"
		}
		if optsField == fields[3] {
			return Ok(name + " options already up to date in " + path), nil
		}
		fields[3] = optsField
		lines[idx] = strings.Join(fields, " ")
		if err := crypttabWriteLines(ctx, conn, path, lines); err != nil {
			return Result{}, err
		}
		return Changed(name + " options updated in " + path), nil

	default:
		return Result{}, errArg("crypttab: state must be present, absent, opts_present, or opts_absent, got %q", state)
	}
}

// crypttabReadLines reads path's raw lines via `cat`, tolerating a
// missing file (returned as no lines, not an error) — matching
// mount.go's own readFstabLines, since crypttab management can
// legitimately run against a fresh file this module is about to create
// the first entry in.
func crypttabReadLines(ctx context.Context, conn remoteexec.Connection, path string) ([]string, error) {
	res, err := runStatus(ctx, conn, "cat "+shellQuote(path)+" 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	return splitLines(res.Stdout), nil
}

func crypttabWriteLines(ctx context.Context, conn remoteexec.Connection, path string, lines []string) error {
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	_, err := conn.Exec(ctx, "cat > "+shellQuote(path), strings.NewReader(content))
	return err
}

// crypttabEntryIndex returns the index of the line whose first
// whitespace-separated field (the device name) equals name, skipping
// blank lines and comments, or -1.
func crypttabEntryIndex(lines []string, name string) int {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 1 && fields[0] == name {
			return i
		}
	}
	return -1
}

// crypttabSplitOpts splits a comma-delimited opts field into its
// individual options, treating "none", "-", and "" (crypttab's own
// placeholders for "no options") as an empty list.
func crypttabSplitOpts(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "none" || s == "-" {
		return nil
	}
	var out []string
	for _, o := range strings.Split(s, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			out = append(out, o)
		}
	}
	return out
}

func crypttabOptKey(opt string) string {
	if i := strings.IndexByte(opt, '='); i >= 0 {
		return opt[:i]
	}
	return opt
}

// crypttabMergeOpts adds each of want to existing, updating (by key,
// the text before "=") any option that's already present with a
// different value rather than duplicating it.
func crypttabMergeOpts(existing, want []string) []string {
	out := append([]string{}, existing...)
	for _, w := range want {
		key := crypttabOptKey(w)
		replaced := false
		for i, o := range out {
			if crypttabOptKey(o) == key {
				out[i] = w
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, w)
		}
	}
	return out
}

// crypttabRemoveOpts drops any option from existing whose key (the text
// before "=") matches one of remove's keys, regardless of remove's own
// value for that key.
func crypttabRemoveOpts(existing, remove []string) []string {
	removeKeys := map[string]bool{}
	for _, r := range remove {
		removeKeys[crypttabOptKey(r)] = true
	}
	var out []string
	for _, o := range existing {
		if !removeKeys[crypttabOptKey(o)] {
			out = append(out, o)
		}
	}
	return out
}
