package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleBtrfsSubvolume implements (a subset of) Ansible's
// `btrfs_subvolume` module: creates, deletes, snapshots, or sets the
// default subvolume of a btrfs filesystem via `btrfs subvolume create/
// delete/snapshot/set-default`, reusing btrfs_info.go's own filesystem/
// subvolume discovery (`btrfs filesystem show`, `btrfs subvolume list
// -a`, `btrfs subvolume get-default`).
//
// Args: name (string, required) — the subvolume's path, relative to
// the filesystem's TOP-LEVEL subvolume (fs root), e.g. "/@home";
// state (present|absent, default "present"); default (bool, default
// false) — makes name the filesystem's default subvolume;
// snapshot_source (string) — creates name as a snapshot of this
// (also fs-root-relative) source subvolume, inferred to mean "create
// name as a snapshot" rather than an empty subvolume; snapshot_conflict
// (skip|clobber|error, default "skip") — behavior when name already
// exists and snapshot_source is given: skip leaves it alone (Ok,
// unchanged), clobber deletes the existing subvolume first (not
// idempotent — a fresh snapshot every run, matching real
// btrfs_subvolume's own documented caveat), error fails; recursive
// (bool, default false) — for state=present, `mkdir -p` name's parent
// directory before creating (rather than requiring it pre-exist); for
// state=absent, deletes any subvolume nested under name (deepest path
// first) before deleting name itself; filesystem_device/
// filesystem_label/filesystem_uuid (string) — selects which btrfs
// filesystem to target when more than one exists; automount (bool,
// default false) — see below.
//
// Locating a usable filesystem PATH: name is relative to the
// filesystem's fs-root (top-level, id 5) subvolume, so this port needs
// an actual mounted path that exposes that root — NOT just any mount of
// the filesystem the way btrfs_info.go's own read-only queries can use.
// This port treats an existing mount of one of the filesystem's devices
// that carries NEITHER a `subvol=` NOR a `subvolid=` option as exposing
// the fs root (real btrfs mounts the filesystem's OWN default
// subvolume when neither option is given, which is usually, but is not
// GUARANTEED to be, id 5 — this is a narrowing, not a silent one: if an
// administrator has changed the target filesystem's default subvolume
// away from its root, this heuristic can misidentify the root view).
// When no such mount exists and automount=true, this port mounts a
// fresh temporary directory with the EXPLICIT option `subvolid=5`
// (guaranteeing the true root regardless of the current default) and
// unmounts+removes it again before returning. When no such mount exists
// and automount=false, this port fails cleanly rather than guessing.
//
// Filesystem selection: if none of filesystem_device/_label/_uuid are
// given and there is exactly one filesystem, that one is used; with
// zero or more than one, and no selector given, this port fails (real
// btrfs_subvolume's own note documents the same "single filesystem or
// error" rule for the no-selector case).
func moduleBtrfsSubvolume(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("btrfs_subvolume: state must be present or absent, got %q", state)
	}
	makeDefault := argBool(args, "default", false)
	snapshotSource := argString(args, "snapshot_source", "")
	snapshotConflict := argString(args, "snapshot_conflict", "skip")
	recursive := argBool(args, "recursive", false)
	automount := argBool(args, "automount", false)

	fs, err := btrfsSelectFilesystem(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	if fs == nil {
		return Fail("btrfs_subvolume: no matching btrfs filesystem found (or more than one, with no filesystem_device/_label/_uuid selector given)"), nil
	}

	mounts, err := gatherMounts(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	root, cleanup, failMsg, err := btrfsEnsureRootMount(ctx, conn, *fs, mounts, automount)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}
	if cleanup != nil {
		defer cleanup()
	}

	abs := btrfsJoin(root, name)
	changed := false

	isSubvol, err := btrfsIsSubvolume(ctx, conn, abs)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !isSubvol {
			return btrfsSubvolumeResult(false, "already absent"), nil
		}
		if recursive {
			if err := btrfsDeleteChildren(ctx, conn, root, name); err != nil {
				return Result{}, err
			}
		}
		if _, err := run(ctx, conn, "btrfs subvolume delete "+shellQuote(abs)); err != nil {
			return Result{}, err
		}
		return btrfsSubvolumeResult(true, "removed"), nil
	}

	// state == "present"
	if isSubvol {
		if snapshotSource != "" {
			switch snapshotConflict {
			case "skip":
				// leave as-is
			case "clobber":
				if _, err := run(ctx, conn, "btrfs subvolume delete "+shellQuote(abs)); err != nil {
					return Result{}, err
				}
				if err := btrfsCreateSnapshot(ctx, conn, root, snapshotSource, abs); err != nil {
					return Result{}, err
				}
				changed = true
			case "error":
				return Fail(name + " already exists (snapshot_conflict=error)"), nil
			default:
				return Result{}, errArg("btrfs_subvolume: snapshot_conflict must be skip, clobber, or error, got %q", snapshotConflict)
			}
		}
	} else {
		exists, err := pathExists(ctx, conn, abs)
		if err != nil {
			return Result{}, err
		}
		if exists {
			return Fail(abs + " exists but is not a btrfs subvolume; refusing to overwrite it"), nil
		}
		if recursive {
			if _, err := run(ctx, conn, "mkdir -p "+shellQuote(btrfsParentDir(abs))); err != nil {
				return Result{}, err
			}
		}
		if snapshotSource != "" {
			if err := btrfsCreateSnapshot(ctx, conn, root, snapshotSource, abs); err != nil {
				return Result{}, err
			}
		} else {
			if _, err := run(ctx, conn, "btrfs subvolume create "+shellQuote(abs)); err != nil {
				return Result{}, err
			}
		}
		changed = true
	}

	id, err := btrfsSubvolumeID(ctx, conn, abs)
	if err != nil {
		return Result{}, err
	}

	if makeDefault {
		curDefault, err := btrfsGetDefaultSubvolume(ctx, conn, root)
		if err != nil {
			return Result{}, err
		}
		if curDefault != id {
			if _, err := run(ctx, conn, "btrfs subvolume set-default "+strconv.Itoa(id)+" "+shellQuote(root)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	r := btrfsSubvolumeResult(changed, "up to date")
	if id > 0 {
		r = r.WithExtra("target_subvolume_id", id)
	}
	return r, nil
}

func btrfsSubvolumeResult(changed bool, msg string) Result {
	if changed {
		return Changed(msg)
	}
	return Ok(msg)
}

// btrfsSelectFilesystem picks the target filesystem per this file's
// doc comment: an explicit selector if given, else the sole filesystem
// if there is exactly one, else nil (ambiguous/none).
func btrfsSelectFilesystem(ctx context.Context, conn remoteexec.Connection, args map[string]any) (*btrfsFS, error) {
	filesystems, err := btrfsListFilesystems(ctx, conn)
	if err != nil {
		return nil, err
	}
	device := argString(args, "filesystem_device", "")
	label := argString(args, "filesystem_label", "")
	uuid := argString(args, "filesystem_uuid", "")
	if device != "" || label != "" || uuid != "" {
		for i := range filesystems {
			fs := &filesystems[i]
			if device != "" && !containsStr(fs.Devices, device) {
				continue
			}
			if label != "" && fs.Label != label {
				continue
			}
			if uuid != "" && fs.UUID != uuid {
				continue
			}
			return fs, nil
		}
		return nil, nil
	}
	if len(filesystems) == 1 {
		return &filesystems[0], nil
	}
	return nil, nil
}

// btrfsEnsureRootMount returns a mountpoint that exposes fs's top-level
// subvolume, per this file's doc comment, plus a cleanup func to call
// (may be nil) if this port mounted a temporary one itself. A non-empty
// failMsg means the request is well-formed but cannot be satisfied
// (the caller returns Fail(failMsg)); a non-nil err means an actual
// infra/command failure occurred.
func btrfsEnsureRootMount(ctx context.Context, conn remoteexec.Connection, fs btrfsFS, mounts []map[string]any, automount bool) (root string, cleanup func(), failMsg string, err error) {
	for _, m := range mounts {
		if m["fstype"] != "btrfs" {
			continue
		}
		dev, _ := m["device"].(string)
		if !containsStr(fs.Devices, dev) {
			continue
		}
		opts, _ := m["options"].([]string)
		if !containsAnyPrefix(opts, "subvol=", "subvolid=") {
			mp, _ := m["mount_point"].(string)
			return mp, nil, "", nil
		}
	}
	if !automount {
		return "", nil, "btrfs_subvolume: no existing mount exposes the filesystem's root subvolume, and automount=false", nil
	}
	if len(fs.Devices) == 0 {
		return "", nil, "btrfs_subvolume: filesystem has no known devices to mount", nil
	}
	tmp, err := run(ctx, conn, "mktemp -d")
	if err != nil {
		return "", nil, "", err
	}
	if _, err := run(ctx, conn, "mount -o subvolid=5 "+shellQuote(fs.Devices[0])+" "+shellQuote(tmp)); err != nil {
		return "", nil, "", err
	}
	cleanupFn := func() {
		_, _ = run(ctx, conn, "umount "+shellQuote(tmp))
		_, _ = run(ctx, conn, "rmdir "+shellQuote(tmp))
	}
	return tmp, cleanupFn, "", nil
}

func containsAnyPrefix(list []string, prefixes ...string) bool {
	for _, s := range list {
		for _, p := range prefixes {
			if strings.HasPrefix(s, p) {
				return true
			}
		}
	}
	return false
}

// btrfsIsSubvolume reports whether path is currently a btrfs
// subvolume, via `btrfs subvolume show`.
func btrfsIsSubvolume(ctx context.Context, conn remoteexec.Connection, path string) (bool, error) {
	res, err := runStatus(ctx, conn, "btrfs subvolume show "+shellQuote(path)+" >/dev/null 2>&1")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// btrfsSubvolumeID looks up path's own subvolume ID via `btrfs
// subvolume show`, returning 0 if it can't be determined.
func btrfsSubvolumeID(ctx context.Context, conn remoteexec.Connection, path string) (int, error) {
	res, err := runStatus(ctx, conn, "btrfs subvolume show "+shellQuote(path)+" 2>/dev/null")
	if err != nil {
		return 0, err
	}
	if res.RC != 0 {
		return 0, nil
	}
	for _, line := range splitLines(res.Stdout) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Subvolume ID:") {
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "Subvolume ID:"))
			id, _ := strconv.Atoi(v)
			return id, nil
		}
	}
	return 0, nil
}

func btrfsCreateSnapshot(ctx context.Context, conn remoteexec.Connection, root, source, destAbs string) error {
	if !strings.HasPrefix(source, "/") {
		source = "/" + source
	}
	srcAbs := btrfsJoin(root, source)
	_, err := run(ctx, conn, "btrfs subvolume snapshot "+shellQuote(srcAbs)+" "+shellQuote(destAbs))
	return err
}

// btrfsJoin joins a mountpoint root with an fs-root-relative path rel
// (which always starts with "/"), avoiding a doubled "/" when root is
// itself "/".
func btrfsJoin(root, rel string) string {
	return strings.TrimSuffix(root, "/") + rel
}

func btrfsParentDir(path string) string {
	idx := strings.LastIndexByte(path, '/')
	if idx <= 0 {
		return "/"
	}
	return path[:idx]
}

// btrfsDeleteChildren deletes every subvolume nested under name
// (fs-root-relative), deepest path first, so state=absent with
// recursive=true can then remove name itself.
func btrfsDeleteChildren(ctx context.Context, conn remoteexec.Connection, root, name string) error {
	subvols, err := btrfsListSubvolumes(ctx, conn, root)
	if err != nil {
		return err
	}
	prefix := strings.TrimPrefix(name, "/") + "/"
	var children []string
	for _, sv := range subvols {
		if strings.HasPrefix(sv.Path, prefix) {
			children = append(children, sv.Path)
		}
	}
	// Deepest (longest) path first, so a child is removed before its
	// own parent among the children.
	for i := 0; i < len(children); i++ {
		for j := i + 1; j < len(children); j++ {
			if len(children[j]) > len(children[i]) {
				children[i], children[j] = children[j], children[i]
			}
		}
	}
	for _, c := range children {
		if _, err := run(ctx, conn, "btrfs subvolume delete "+shellQuote(btrfsJoin(root, "/"+c))); err != nil {
			return err
		}
	}
	return nil
}
