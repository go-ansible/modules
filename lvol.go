package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLvol implements (a subset of) Ansible's `lvol` module: creates,
// removes, resizes, or (de)activates an LVM logical volume, a thin
// volume, a thin pool, or a snapshot, via `lvcreate`/`lvresize`/
// `lvremove`/`lvchange`.
//
// Args: vg (string, required); lv (string) — the LV name; thinpool
// (string) — a thin pool's name (create/manage the pool itself when
// `lv` is empty, or create a thin volume named `lv` INSIDE it when
// both are given); snapshot (string) — creates a snapshot of `lv` named
// `snapshot`. One of lv/thinpool/snapshot is required. size (string) —
// required to create a new volume; accepted forms match real lvol's
// own documented lvcreate(8)/lvresize(8) syntax: a bare number
// (megabytes) or number+unit ([bBsSkKmMgGtTpPeE]); `N%VG`/`N%PVS`/
// `N%FREE`/`N%ORIGIN` (percentage-based, NOT idempotent — a resize is
// always attempted); or a `+`/`-` prefixed absolute delta (also NOT
// idempotent), matching real lvol's own documented caveats for both
// forms. state (present|absent, default "present"); force (bool,
// default false) — required to actually shrink or remove a volume;
// shrink (bool, default true) — if false, a plain (non-%,non-+/-) size
// smaller than the volume's current size is silently left alone rather
// than shrunk; opts (string, default "") — inserted verbatim into
// `lvcreate` (see filesystem.go's doc comment for this port's house
// convention on free-form opts arguments); pvs (list of string) —
// physical volumes to constrain a NEW plain LV's placement to; active
// (bool, default true) — `lvchange -ay`/`-an`; resizefs (bool, default
// false) — passed straight through as lvresize/lvextend's own native
// `-r`/`--resizefs` flag, rather than this port composing a separate
// resize2fs/xfs_growfs call the way filesystem.go's own resizefs does
// — lvresize already knows how to do this for the filesystem types it
// supports, so this port defers to it.
//
// Resizing: this port always uses `lvresize` — which lvm(8) itself
// describes as the combined front-end for lvextend/lvreduce — rather
// than switching between the two real tools by direction; `-f` is added
// when force=true (required for an actual shrink) and `-r` when
// resizefs=true. This is a simplification of tool choice, not a gap in
// what's achievable through this port's shell-composition architecture.
//
// Existence/size/active-state are read via a single `lvs --noheadings
// -o lv_size,lv_attr --units b --nosuffix vg/name`; lv_attr's 5th
// character ('a' means active) is this port's only use of that field —
// no other attribute (open count, health, etc.) is interpreted.
func moduleLvol(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	vg, err := requireString(args, "vg")
	if err != nil {
		return Result{}, err
	}
	lv := argString(args, "lv", "")
	thinpool := argString(args, "thinpool", "")
	snapshot := argString(args, "snapshot", "")
	if lv == "" && thinpool == "" && snapshot == "" {
		return Result{}, errArg("lvol: one of lv, thinpool, or snapshot is required")
	}
	state := argString(args, "state", "present")
	force := argBool(args, "force", false)

	managedName := snapshot
	if managedName == "" {
		if lv != "" {
			managedName = lv
		} else {
			managedName = thinpool
		}
	}
	path := vg + "/" + managedName

	exists, sizeBytes, attr, err := lvolInfo(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(path + " already absent"), nil
		}
		if !force {
			return Fail(path + " exists; removing it requires force=true"), nil
		}
		if _, err := run(ctx, conn, "lvremove -f "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		return Changed(path + " removed"), nil
	}
	if state != "present" {
		return Result{}, errArg("lvol: state must be present or absent, got %q", state)
	}

	opts := argString(args, "opts", "")
	pvs := argStringList(args, "pvs")
	resizefs := argBool(args, "resizefs", false)
	shrink := argBool(args, "shrink", true)
	active := argBool(args, "active", true)
	size := argString(args, "size", "")

	changed := false
	if !exists {
		if size == "" {
			return Result{}, errArg("lvol: size is required to create %q", path)
		}
		cmd := lvolCreateCmd(vg, lv, thinpool, snapshot, size, opts, pvs)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	} else if size != "" {
		relative := strings.ContainsAny(size, "%+-")
		target, parsed := parseLVSize(size)
		doResize := relative || !parsed
		shrinking := false
		if !relative && parsed {
			const tolerance = 1 << 20 // 1MiB: LVM rounds to extent boundaries
			switch {
			case target > sizeBytes+tolerance:
				doResize = true
			case target < sizeBytes-tolerance:
				shrinking = true
				if !shrink {
					doResize = false
				} else {
					doResize = true
				}
			default:
				doResize = false
			}
		}
		if doResize {
			if shrinking && !force {
				return Fail(path + " would shrink; this requires force=true"), nil
			}
			cmd := "lvresize --size " + size
			if strings.Contains(size, "%") {
				cmd = "lvresize --extents " + size
			}
			if force {
				cmd += " -f"
			}
			if resizefs {
				cmd += " -r"
			}
			cmd += " " + shellQuote(path)
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	// Re-check active state after any create/resize above; a freshly
	// created LV is active by default in real lvcreate's own behavior.
	curActive := active // assume the desired state right after creation
	if exists {
		curActive = len(attr) >= 5 && strings.ToLower(attr[4:5]) == "a"
	}
	if curActive != active {
		flag := "-ay"
		if !active {
			flag = "-an"
		}
		if _, err := run(ctx, conn, "lvchange "+flag+" "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed(path + " updated"), nil
	}
	return Ok(path + " already up to date"), nil
}

func lvolCreateCmd(vg, lv, thinpool, snapshot, size, opts string, pvs []string) string {
	sizeArg := "--size " + size
	if strings.Contains(size, "%") {
		sizeArg = "--extents " + size
	}
	var cmd string
	switch {
	case snapshot != "":
		cmd = "lvcreate -s " + sizeArg + " -n " + shellQuote(snapshot)
		if opts != "" {
			cmd += " " + opts
		}
		cmd += " " + shellQuote(vg+"/"+lv)
	case thinpool != "" && lv != "":
		cmd = "lvcreate -T " + shellQuote(vg+"/"+thinpool) + " -V " + size + " -n " + shellQuote(lv)
		if opts != "" {
			cmd += " " + opts
		}
	case thinpool != "":
		cmd = "lvcreate -T " + sizeArg + " " + shellQuote(vg+"/"+thinpool)
		if opts != "" {
			cmd += " " + opts
		}
	default:
		cmd = "lvcreate " + sizeArg + " -n " + shellQuote(lv)
		if opts != "" {
			cmd += " " + opts
		}
		cmd += " " + shellQuote(vg)
		if len(pvs) > 0 {
			cmd += " " + strings.Join(quotedList(pvs), " ")
		}
	}
	return cmd
}

// lvolInfo reports whether path (vg/lv) exists and, if so, its size in
// bytes and its raw lv_attr string.
func lvolInfo(ctx context.Context, conn remoteexec.Connection, path string) (exists bool, sizeBytes int64, attr string, err error) {
	res, err := runStatus(ctx, conn, "lvs --noheadings -o lv_size,lv_attr --units b --nosuffix "+shellQuote(path)+" 2>/dev/null")
	if err != nil {
		return false, 0, "", err
	}
	if res.RC != 0 {
		return false, 0, "", nil
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) < 2 {
		return false, 0, "", nil
	}
	sizeBytes, _ = strconv.ParseInt(fields[0], 10, 64)
	return true, sizeBytes, fields[1], nil
}

// parseLVSize parses a plain lvcreate(8)/lvresize(8) size (a bare
// number, defaulting to megabytes, or number+unit suffix from
// [bBsSkKmMgGtTpPeE]) into bytes. It does NOT handle percentage or
// +/- relative forms — callers check for those separately.
func parseLVSize(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	unit := byte('m')
	numPart := s
	last := s[len(s)-1]
	if (last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') {
		unit = lowerByte(last)
		numPart = s[:len(s)-1]
	}
	f, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, false
	}
	mult, ok := lvSizeMult[unit]
	if !ok {
		return 0, false
	}
	return int64(f * float64(mult)), true
}

var lvSizeMult = map[byte]int64{
	'b': 1, 's': 512, 'k': 1024, 'm': 1024 * 1024, 'g': 1024 * 1024 * 1024,
	't': 1 << 40, 'p': 1 << 50, 'e': 1 << 60,
}

func lowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
