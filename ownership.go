package modules

import (
	"context"
	"fmt"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// chownSpec is the chown argument that gives a file at fi the wanted
// owner and group, or "" when it already has both. An empty owner or
// group is "not asked for", as it is everywhere else in this package.
// Either may be a name or a numeric ID, as real's set_owner_if_different
// and set_group_if_different accept both. Only the half that differs is
// named ("alice", ":staff" or "alice:staff"), so chown does no more than
// was asked.
//
// A missing file (fi == nil), or a stat that did not report ownership
// (owner and group both empty -- statPath tolerates that shape), cannot
// be compared, so the whole request is passed on rather than guessed to
// be satisfied.
func chownSpec(fi *fileInfo, owner, group string) string {
	known := fi != nil && (fi.owner != "" || fi.group != "")
	ownerDiffers := owner != "" && !(known && (owner == fi.owner || owner == strconv.Itoa(fi.uid)))
	groupDiffers := group != "" && !(known && (group == fi.group || group == strconv.Itoa(fi.gid)))
	switch {
	case ownerDiffers && groupDiffers:
		return owner + ":" + group
	case ownerDiffers:
		return owner
	case groupDiffers:
		return ":" + group
	}
	return ""
}

// applyFileAttrs brings path's owner, group and mode to what was asked,
// changing only what differs, and reports whether anything did (or, in
// check mode, would). It is the part of real's
// set_fs_attributes_if_different that copy and template need, in real's
// order: owner and group first, then mode -- chown on Linux can clear
// the setuid and setgid bits of an executable, so a mode applied before
// it could be undone by it. That is also why the mode is compared against a fresh stat when a
// chown ran.
func applyFileAttrs(ctx context.Context, conn remoteexec.Connection, path string, mode *uint32, owner, group string, check bool) (bool, error) {
	if mode == nil && owner == "" && group == "" {
		return false, nil
	}
	info, err := statPath(ctx, conn, path)
	if err != nil {
		return false, err
	}
	changed := false
	if spec := chownSpec(info, owner, group); spec != "" {
		if check {
			return true, nil
		}
		if _, err := run(ctx, conn, "chown "+shellQuote(spec)+" "+shellQuote(path)); err != nil {
			return false, err
		}
		changed = true
		if mode != nil {
			if info, err = statPath(ctx, conn, path); err != nil {
				return false, err
			}
		}
	}
	if mode != nil && (info == nil || info.mode != *mode) {
		if check {
			return true, nil
		}
		if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(path))); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
