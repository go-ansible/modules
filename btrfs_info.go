package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// btrfsFS is one filesystem as reported by `btrfs filesystem show`.
type btrfsFS struct {
	UUID    string
	Label   string
	Devices []string
}

// btrfsSubvol is one subvolume as reported by `btrfs subvolume list
// -a`, plus the mountpoints (if any) this port has attributed to it.
type btrfsSubvol struct {
	ID          int
	Parent      int
	Path        string
	Mountpoints []string
}

// moduleBtrfsInfo implements Ansible's `btrfs_info` module: a
// read-only query of every btrfs filesystem this port can find via
// `btrfs filesystem show`, plus (for each filesystem this port can
// also find a mountpoint for) its default subvolume and full subvolume
// list via `btrfs subvolume get-default`/`btrfs subvolume list -a`.
// Never reports Changed, matching real btrfs_info's own read-only
// nature.
//
// No arguments. Returns Extra["filesystems"], a list of dicts with
// uuid/label/devices always present, and default_subvolume/subvolumes
// present only when this port also located a mountpoint for that
// filesystem — matching real btrfs_info's own documented "returned:
// success and if filesystem is mounted" on those two fields.
//
// Each subvolume dict carries id/parent/path/mountpoints. mountpoints
// is populated by matching /proc/mounts entries against this
// filesystem's own device list and reading each match's `subvolid=N`
// mount option (a mount with NO subvolid option is attributed to the
// filesystem's own default subvolume, per btrfs's own documented mount
// default). A mount using the alternate `subvol=/path` option (by path
// rather than numeric id) is NOT matched by this port — real btrfs
// supports both interchangeably; this port only reads the numeric
// form, a narrowing rather than a silent miss (such a mount is simply
// absent from that subvolume's mountpoints list here).
func moduleBtrfsInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	filesystems, err := btrfsListFilesystems(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	mounts, err := gatherMounts(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	out := make([]any, 0, len(filesystems))
	for _, fs := range filesystems {
		entry := map[string]any{
			"uuid": fs.UUID, "label": fs.Label, "devices": fs.Devices,
		}
		mnt, ok := btrfsAnyMountpoint(mounts, fs)
		if ok {
			defID, err := btrfsGetDefaultSubvolume(ctx, conn, mnt)
			if err != nil {
				return Result{}, err
			}
			subvols, err := btrfsListSubvolumes(ctx, conn, mnt)
			if err != nil {
				return Result{}, err
			}
			btrfsAttributeMountpoints(subvols, mounts, fs, defID)
			entry["default_subvolume"] = defID
			entry["subvolumes"] = btrfsSubvolsToAny(subvols)
		}
		out = append(out, entry)
	}
	return Ok("btrfs_info").WithExtra("filesystems", out), nil
}

// btrfsListFilesystems runs and parses `btrfs filesystem show`.
func btrfsListFilesystems(ctx context.Context, conn remoteexec.Connection) ([]btrfsFS, error) {
	res, err := runStatus(ctx, conn, "btrfs filesystem show 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	return btrfsParseFilesystemShow(res.Stdout), nil
}

func btrfsParseFilesystemShow(out string) []btrfsFS {
	var list []btrfsFS
	var cur *btrfsFS
	for _, line := range splitLines(out) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			cur = nil
			continue
		}
		if strings.HasPrefix(trimmed, "Label:") {
			list = append(list, btrfsFS{})
			cur = &list[len(list)-1]
			cur.Label = btrfsParseLabel(trimmed)
			if idx := strings.Index(trimmed, "uuid:"); idx >= 0 {
				cur.UUID = strings.TrimSpace(trimmed[idx+len("uuid:"):])
			}
			continue
		}
		if cur == nil {
			continue
		}
		if idx := strings.Index(trimmed, "path "); idx >= 0 && strings.HasPrefix(trimmed, "devid") {
			cur.Devices = append(cur.Devices, strings.TrimSpace(trimmed[idx+len("path "):]))
		}
	}
	return list
}

// btrfsParseLabel extracts the quoted (or "none") label from a
// `Label: 'Tank'  uuid: ...` header line.
func btrfsParseLabel(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "Label:"))
	if strings.HasPrefix(rest, "'") {
		if end := strings.Index(rest[1:], "'"); end >= 0 {
			return rest[1 : end+1]
		}
	}
	if idx := strings.Index(rest, "uuid:"); idx >= 0 {
		return strings.TrimSpace(rest[:idx])
	}
	return rest
}

// btrfsAnyMountpoint returns any current mountpoint of fs (matched by
// device membership), regardless of which subvolume is mounted there
// — sufficient for read-only queries like `subvolume list -a`, which
// work relative to the whole filesystem from any mounted path within
// it.
func btrfsAnyMountpoint(mounts []map[string]any, fs btrfsFS) (string, bool) {
	for _, m := range mounts {
		if m["fstype"] != "btrfs" {
			continue
		}
		dev, _ := m["device"].(string)
		if containsStr(fs.Devices, dev) {
			mp, _ := m["mount_point"].(string)
			return mp, mp != ""
		}
	}
	return "", false
}

// btrfsGetDefaultSubvolume runs `btrfs subvolume get-default mnt` and
// parses out the subvolume's numeric ID.
func btrfsGetDefaultSubvolume(ctx context.Context, conn remoteexec.Connection, mnt string) (int, error) {
	res, err := runStatus(ctx, conn, "btrfs subvolume get-default "+shellQuote(mnt)+" 2>/dev/null")
	if err != nil {
		return 0, err
	}
	if res.RC != 0 {
		return 0, nil
	}
	fields := strings.Fields(strings.TrimSpace(res.Stdout))
	if len(fields) >= 2 && fields[0] == "ID" {
		id, _ := strconv.Atoi(fields[1])
		return id, nil
	}
	return 0, nil
}

// btrfsListSubvolumes runs and parses `btrfs subvolume list -a mnt`.
func btrfsListSubvolumes(ctx context.Context, conn remoteexec.Connection, mnt string) ([]btrfsSubvol, error) {
	res, err := runStatus(ctx, conn, "btrfs subvolume list -a "+shellQuote(mnt)+" 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	return btrfsParseSubvolumeList(res.Stdout), nil
}

func btrfsParseSubvolumeList(out string) []btrfsSubvol {
	var list []btrfsSubvol
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		if fields[0] != "ID" || fields[2] != "gen" || fields[4] != "top" || fields[5] != "level" || fields[7] != "path" {
			continue
		}
		id, _ := strconv.Atoi(fields[1])
		parent, _ := strconv.Atoi(fields[6])
		path := strings.Join(fields[8:], " ")
		list = append(list, btrfsSubvol{ID: id, Parent: parent, Path: path})
	}
	return list
}

// btrfsAttributeMountpoints fills in each subvolume's Mountpoints by
// scanning mounts for entries belonging to fs: a `subvolid=N` mount
// option attributes that mountpoint to subvolume N; an entry with
// neither `subvolid=` nor `subvol=` is attributed to defID (the
// filesystem's own default subvolume, which is what a plain,
// option-less mount of a btrfs device exposes).
func btrfsAttributeMountpoints(subvols []btrfsSubvol, mounts []map[string]any, fs btrfsFS, defID int) {
	byID := make(map[int]*btrfsSubvol, len(subvols))
	for i := range subvols {
		byID[subvols[i].ID] = &subvols[i]
	}
	for _, m := range mounts {
		if m["fstype"] != "btrfs" {
			continue
		}
		dev, _ := m["device"].(string)
		if !containsStr(fs.Devices, dev) {
			continue
		}
		mp, _ := m["mount_point"].(string)
		opts, _ := m["options"].([]string)
		id := defID
		for _, o := range opts {
			if strings.HasPrefix(o, "subvolid=") {
				if n, err := strconv.Atoi(strings.TrimPrefix(o, "subvolid=")); err == nil {
					id = n
				}
			}
		}
		if sv, ok := byID[id]; ok {
			sv.Mountpoints = append(sv.Mountpoints, mp)
		}
	}
}

func btrfsSubvolsToAny(subvols []btrfsSubvol) []any {
	out := make([]any, 0, len(subvols))
	for _, s := range subvols {
		mps := s.Mountpoints
		if mps == nil {
			mps = []string{}
		}
		out = append(out, map[string]any{
			"id": s.ID, "parent": s.Parent, "path": s.Path, "mountpoints": mps,
		})
	}
	return out
}
