package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFilesystem implements (a subset of) Ansible's `filesystem`
// module: creates, grows, or wipes a filesystem on a block device (or
// regular file) via the target's own `mkfs.*`/`mkswap`/`pvcreate`
// tools, `blkid` (to detect the current signature), and a per-fstype
// resize/UUID tool.
//
// Args: dev (string, required, aliased from device); fstype (string,
// aliased from type, required when state=present) — one of bcachefs,
// btrfs, ext2, ext3, ext4, ext4dev, f2fs, lvm, ocfs2, reiserfs, xfs,
// vfat, swap, ufs, gfs2, matching real filesystem's own documented
// list; force (bool, default false) — allows overwriting a device that
// already carries a different filesystem; opts (string, default "") —
// inserted VERBATIM (not shell-quoted) after the force flag, matching
// this port's house convention for free-form CLI-flag arguments (see
// lvg.go/lvol.go's own pv_options/opts for the same treatment) — the
// caller is trusted the same way real Ansible trusts its own module
// args; label (string) — only honored for fstype=gfs2, via
// `mkfs.gfs2 -t CLUSTERNAME:LOCKSPACE`, matching real filesystem's own
// narrow documented support; uuid (string) — resets the filesystem's UUID,
// mutually exclusive with resizefs, NOT idempotent (always reports
// changed when given, matching real filesystem's own documented
// behavior); resizefs (bool, default false) — grows the filesystem to
// fill dev; state (present|absent, default "present").
//
// Detection: the current filesystem signature is read via `blkid -o
// value -s TYPE dev`. state=present with no existing signature creates
// one; a matching existing signature is a no-op (subject to
// resizefs/uuid below); a MISMATCHED existing signature fails unless
// force=true, which re-creates it. state=absent wipes dev's signature
// via `wipefs -a` when blkid reports one, does nothing if blkid reports
// none, and — unlike real filesystem's own documented "still wiped even
// if blkid can't detect a type, even with force=false" edge case — this
// port trusts blkid's answer either way, which is a narrowing: a
// filesystem blkid genuinely cannot identify is left alone here, rather
// than wiped blind. state=absent does not fail if dev does not exist
// (matching real behavior); state=present does fail on a missing dev,
// since there's nothing to run mkfs against.
//
// Force-flag and resize/UUID tool tables (see fsForceFlag/fsGrowCmd/
// fsUUIDCmd below) are this port's own best-effort mapping of each
// fstype to its mkfs tool's own force flag and its own resize/tune
// tool, assembled from each tool's own documented convention rather
// than from real filesystem's own source (which this port's
// architecture cannot reuse, being Python) — treat it as a reasonable
// approximation, not a byte-for-byte port. resizefs is only implemented
// for ext2/ext3/ext4/ext4dev (`resize2fs`), xfs (`xfs_growfs`), btrfs
// (`btrfs filesystem resize max`), and lvm (`pvresize`) — the four
// most common real-world cases; vfat/f2fs/ocfs2/bcachefs/gfs2/ufs
// resize is NOT implemented (fails cleanly with a clear message) since
// their resize tools (`fatresize`, `resize.f2fs`, ...) are less
// consistently available and their exact invocation is less
// standardized. uuid is only implemented for ext2/ext3/ext4/ext4dev
// (`tune2fs -U`), xfs (`xfs_admin -U`), and lvm (`pvchange -u`, which
// ignores the given value and always regenerates the PV UUID, matching
// real filesystem's own documented "for fstype=lvm the value is
// ignored, it resets the PV UUID if set") — other fstypes fail cleanly.
// Note also that xfs_growfs/`btrfs filesystem resize` both really
// operate on a MOUNT POINT, not a block device; this port passes dev
// directly (matching real filesystem's own documented "XFS only grows
// if mounted" constraint) rather than resolving dev to its current
// mount point the way real filesystem does internally — if dev is not
// itself currently mounted at a path usable by the tool, the resize
// command fails with the tool's own error, which surfaces honestly
// rather than silently no-op'ing.
func moduleFilesystem(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dev := argString(args, "dev", argString(args, "device", ""))
	if dev == "" {
		return Result{}, errArg("filesystem: dev (or its alias device) is required")
	}
	state := argString(args, "state", "present")

	switch state {
	case "absent":
		exists, err := pathExists(ctx, conn, dev)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(dev + " does not exist"), nil
		}
		typ, err := blkidType(ctx, conn, dev)
		if err != nil {
			return Result{}, err
		}
		if typ == "" {
			return Ok(dev + " has no detectable filesystem signature"), nil
		}
		if _, err := run(ctx, conn, "wipefs -a "+shellQuote(dev)); err != nil {
			return Result{}, err
		}
		return Changed(dev + " filesystem signature wiped"), nil

	case "present":
		fstype := argString(args, "fstype", argString(args, "type", ""))
		if fstype == "" {
			return Result{}, errArg("filesystem: fstype (or its alias type) is required when state is present")
		}
		force := argBool(args, "force", false)
		opts := argString(args, "opts", "")
		label := argString(args, "label", "")
		resizefs := argBool(args, "resizefs", false)
		uuid := argString(args, "uuid", "")
		if resizefs && uuid != "" {
			return Result{}, errArg("filesystem: resizefs and uuid are mutually exclusive")
		}

		exists, err := pathExists(ctx, conn, dev)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Result{}, errArg("filesystem: dev %q does not exist", dev)
		}

		cur, err := blkidType(ctx, conn, dev)
		if err != nil {
			return Result{}, err
		}

		if cur == "" || (cur != fstype && force) {
			if _, err := run(ctx, conn, fsMkfsCmd(fstype, dev, opts, label, force)); err != nil {
				return Result{}, err
			}
			return Changed(dev + " filesystem created (" + fstype + ")"), nil
		}
		if cur != fstype {
			return Fail(dev + " already has a " + cur + " filesystem (fstype=" + fstype + " requested); use force=true to overwrite"), nil
		}

		// Already the requested fstype.
		changed := false
		if resizefs {
			cmd, ok := fsGrowCmd(fstype, dev)
			if !ok {
				return Fail("filesystem: resizefs is not implemented by this port for fstype " + fstype), nil
			}
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true
		}
		if uuid != "" {
			cmd, ok := fsUUIDCmd(fstype, dev, uuid)
			if !ok {
				return Fail("filesystem: uuid is not implemented by this port for fstype " + fstype), nil
			}
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true // not idempotent, matching real filesystem's own documented behavior
		}
		if changed {
			return Changed(dev + " filesystem updated"), nil
		}
		return Ok(dev + " already has a " + fstype + " filesystem"), nil

	default:
		return Result{}, errArg("filesystem: state must be present or absent, got %q", state)
	}
}

// blkidType returns dev's current filesystem/PV signature type as
// reported by `blkid`, or "" if blkid detects none (including when dev
// does not exist or blkid itself is absent — both surface as an empty
// type rather than an error, since "no signature" is exactly the
// meaningful answer this helper's callers need).
func blkidType(ctx context.Context, conn remoteexec.Connection, dev string) (string, error) {
	res, err := runStatus(ctx, conn, "blkid -o value -s TYPE "+shellQuote(dev)+" 2>/dev/null")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	return strings.TrimSpace(res.Stdout), nil
}

// fsMkfsCmd builds the mkfs-family command line for fstype.
func fsMkfsCmd(fstype, dev, opts, label string, force bool) string {
	var tool string
	switch fstype {
	case "swap":
		tool = "mkswap"
	case "lvm":
		tool = "pvcreate"
	case "ufs":
		tool = "newfs"
	default:
		tool = "mkfs." + fstype
	}
	cmd := tool
	if force {
		if flag, ok := fsForceFlag[fstype]; ok {
			cmd += " " + flag
		}
	}
	if fstype == "gfs2" && label != "" {
		cmd += " -t " + shellQuote(label)
	}
	if opts != "" {
		cmd += " " + opts
	}
	cmd += " " + shellQuote(dev)
	return cmd
}

// fsForceFlag is this port's own best-effort mapping of each fstype to
// its mkfs tool's own "overwrite existing signature" flag — see this
// file's doc comment for the caveats on this table.
var fsForceFlag = map[string]string{
	"ext2": "-F", "ext3": "-F", "ext4": "-F", "ext4dev": "-F",
	"xfs": "-f", "btrfs": "-f", "reiserfs": "-f", "f2fs": "-f",
	"ocfs2": "-F", "bcachefs": "-f", "gfs2": "-O",
	"vfat": "-I", "swap": "-f", "lvm": "-f",
}

// fsGrowCmd returns the resize command for fstype, or ok=false if this
// port does not implement resizing that fstype.
func fsGrowCmd(fstype, dev string) (string, bool) {
	switch fstype {
	case "ext2", "ext3", "ext4", "ext4dev":
		return "resize2fs " + shellQuote(dev), true
	case "xfs":
		return "xfs_growfs " + shellQuote(dev), true
	case "btrfs":
		return "btrfs filesystem resize max " + shellQuote(dev), true
	case "lvm":
		return "pvresize " + shellQuote(dev), true
	default:
		return "", false
	}
}

// fsUUIDCmd returns the UUID-setting command for fstype, or ok=false if
// this port does not implement UUID setting for that fstype.
func fsUUIDCmd(fstype, dev, uuid string) (string, bool) {
	switch fstype {
	case "ext2", "ext3", "ext4", "ext4dev":
		return "tune2fs -U " + shellQuote(uuid) + " " + shellQuote(dev), true
	case "xfs":
		return "xfs_admin -U " + shellQuote(uuid) + " " + shellQuote(dev), true
	case "lvm":
		return "pvchange -u " + shellQuote(dev), true
	default:
		return "", false
	}
}
