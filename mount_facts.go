package modules

import (
	"context"
	"path"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMountFacts implements (a subset of) Ansible's `mount_facts`
// module: gathers the target's currently mounted filesystems into
// Extra["mounts"], a list of maps each with "device", "mount_point",
// "fstype", and "options".
//
// Args: devices, fstypes ([]string, optional) — glob patterns filtering
// the result by device or filesystem type, matched with Go's
// path.Match, which is close to (but not identical to) real
// mount_facts' Python fnmatch — both support `*`/`?`/`[...]`, but
// path.Match additionally treats `/` specially (it won't let `*` match
// across a `/`), which rarely matters for the flat device/fstype
// strings this filters against.
//
// This port tries Linux's /proc/mounts first (one cat, trivially
// parsed: whitespace-separated device/mount_point/fstype/options/dump/
// pass, matching stat.go's own GNU-first/BSD-fallback pattern), then
// falls back to parsing plain `mount` command output for targets
// without /proc/mounts (macOS/*BSD). Real mount_facts supports many
// more `sources` (/etc/mtab, /etc/fstab, /etc/vfstab, getmntent, etc.),
// selectable and orderable via its own `sources` argument, plus
// aggregate_mounts, mount_binary, on_timeout/timeout handling — none of
// that is implemented here; this port always tries exactly the two
// sources above, in that fixed order, and fails cleanly if neither
// works, rather than replicating that whole source-priority system.
func moduleMountFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	devicePatterns := argStringList(args, "devices")
	fstypePatterns := argStringList(args, "fstypes")

	mounts, err := gatherMounts(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	if len(devicePatterns) > 0 || len(fstypePatterns) > 0 {
		mounts = filterMounts(mounts, devicePatterns, fstypePatterns)
	}
	return Ok("").WithExtra("mounts", mounts), nil
}

// gatherMounts tries /proc/mounts, then falls back to `mount`.
func gatherMounts(ctx context.Context, conn remoteexec.Connection) ([]map[string]any, error) {
	res, err := runStatus(ctx, conn, "cat /proc/mounts 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if res.RC == 0 && strings.TrimSpace(res.Stdout) != "" {
		return parseProcMounts(res.Stdout), nil
	}

	res, err = runStatus(ctx, conn, "mount")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, errArg("mount_facts: neither /proc/mounts nor `mount` produced usable output")
	}
	return parseMountCommand(res.Stdout), nil
}

// parseProcMounts parses /proc/mounts's fixed whitespace-separated
// format: device mount_point fstype options dump pass.
func parseProcMounts(out string) []map[string]any {
	var mounts []map[string]any
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mounts = append(mounts, map[string]any{
			"device":      fields[0],
			"mount_point": fields[1],
			"fstype":      fields[2],
			"options":     strings.Split(fields[3], ","),
		})
	}
	return mounts
}

// parseMountCommand parses plain `mount` output, in either of its two
// common shapes:
//
//	BSD/macOS:      device on mount_point (fstype, opt1, opt2)
//	Linux util-linux: device on mount_point type fstype (opt1,opt2)
func parseMountCommand(out string) []map[string]any {
	var mounts []map[string]any
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		device, rest, ok := strings.Cut(line, " on ")
		if !ok {
			continue
		}
		mountPoint, paren, ok := strings.Cut(rest, " (")
		if !ok {
			continue
		}
		paren = strings.TrimSuffix(paren, ")")

		var fstype string
		var optsStr string
		if t, o, ok := strings.Cut(mountPoint, " type "); ok {
			// Linux shape: mount_point ends with " type fstype", and
			// paren holds only the options.
			mountPoint = t
			fstype = o
			optsStr = paren
		} else if f, o, ok := strings.Cut(paren, ", "); ok {
			// BSD/macOS shape: paren holds "fstype, opt1, opt2".
			fstype = f
			optsStr = o
		} else {
			fstype = paren
		}

		var opts []string
		if optsStr != "" {
			for _, o := range strings.Split(optsStr, ",") {
				opts = append(opts, strings.TrimSpace(o))
			}
		}
		mounts = append(mounts, map[string]any{
			"device":      device,
			"mount_point": mountPoint,
			"fstype":      fstype,
			"options":     opts,
		})
	}
	return mounts
}

// filterMounts keeps only entries whose device matches one of
// devicePatterns (when given) AND whose fstype matches one of
// fstypePatterns (when given).
func filterMounts(mounts []map[string]any, devicePatterns, fstypePatterns []string) []map[string]any {
	var out []map[string]any
	for _, m := range mounts {
		if len(devicePatterns) > 0 && !matchesAny(devicePatterns, m["device"].(string)) {
			continue
		}
		if len(fstypePatterns) > 0 && !matchesAny(fstypePatterns, m["fstype"].(string)) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func matchesAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if ok, err := path.Match(p, s); err == nil && ok {
			return true
		}
	}
	return false
}
