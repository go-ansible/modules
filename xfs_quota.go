package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXfsQuota implements (a subset of) Ansible's `xfs_quota` module:
// configures user/group/project quotas on an XFS filesystem via the
// `xfs_quota -x -c '<subcommand>' <mountpoint>` expert-mode CLI.
//
// Args: type (user|group|project, required); name (string, optional) —
// the user/group/project to limit; defaults to "root" for user/group
// and "#0" (the default project) for project, matching real xfs_quota
// exactly; mountpoint (string, required); bhard/bsoft/rtbhard/rtbsoft
// (string, optional) — block/realtime-block limits, accepting a
// human-readable size (e.g. "1g") exactly like real xfs_quota's own
// human_to_bytes-based parsing (this port reuses filesize.go's own
// filesizeParseBytes helper for that, treating a suffix-less value as
// a raw byte count rather than filesize.go's own "blocks" default —
// matching real xfs_quota's own human_to_bytes semantics, not
// filesize.go's); ihard/isoft (int, optional) — inode limits; state
// (present|absent, default "present") — absent sets every limit to 0
// (matching real xfs_quota exactly: "removing" a quota sets it to 0,
// it is not literally deleted).
//
// Real xfs_quota verifies the target is actually a mounted XFS
// filesystem with the relevant quota mount option (uquota/usrquota/
// quota/uqnoenforce/qnoenforce for user, gquota/grpquota/gqnoenforce
// for group, pquota/prjquota/pqnoenforce for project) by reading
// /proc/mounts and calling pwd.getpwnam/grp.getgrnam locally — since a
// real Ansible module runs ON the target, those checks run on the
// SAME host as xfs_quota itself; this port composes the equivalent
// checks as shell commands over the Connection instead (an `awk` read
// of /proc/mounts, and `getent passwd`/`getent group` in place of
// pwd/grp), so they still run against the actual target, not the
// control node. A project name other than the default additionally
// requires an `/etc/projid` entry (checked via `grep`) and gets
// `project -s <name>` run first if not yet associated with the
// mountpoint (mirroring real xfs_quota's own project-association
// step) — `/etc/projects` itself is NOT independently verified to
// exist (real xfs_quota checks this only to produce a clearer error
// message; xfs_quota's own `project` subcommand fails on a missing
// file regardless, so this port lets that failure surface via the
// command's own exit code instead of pre-checking).
//
// Current limits are read via `xfs_quota -x -c 'report -<type> -<b|i|
// r>' <mountpoint>`, parsed by matching the row whose first field is
// name (matching real xfs_quota's own quota_report parse exactly,
// including its ×1024 factor for block/realtime-block rows). Only
// limits that differ from their current value are included in the
// `limit ...` call that follows, and the default entity's limits are
// set via `limit -<type> -d ...` (no name) rather than `limit -<type>
// ... <name>`, matching real xfs_quota's own default-vs-named branch.
func moduleXfsQuota(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	quotaType, err := requireString(args, "type")
	if err != nil {
		return Result{}, err
	}
	var typeArg, defaultName string
	switch quotaType {
	case "user":
		typeArg, defaultName = "-u", "root"
	case "group":
		typeArg, defaultName = "-g", "root"
	case "project":
		typeArg, defaultName = "-p", "#0"
	default:
		return Result{}, errArg("xfs_quota: type must be user, group, or project, got %q", quotaType)
	}
	mountpoint, err := requireString(args, "mountpoint")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("xfs_quota: state must be present or absent, got %q", state)
	}
	name := argString(args, "name", defaultName)

	bhard, bhardSet, err := xfsQuotaSizeArg(args, "bhard")
	if err != nil {
		return Result{}, err
	}
	bsoft, bsoftSet, err := xfsQuotaSizeArg(args, "bsoft")
	if err != nil {
		return Result{}, err
	}
	rtbhard, rtbhardSet, err := xfsQuotaSizeArg(args, "rtbhard")
	if err != nil {
		return Result{}, err
	}
	rtbsoft, rtbsoftSet, err := xfsQuotaSizeArg(args, "rtbsoft")
	if err != nil {
		return Result{}, err
	}
	ihard, ihardSet := xfsQuotaIntArg(args, "ihard")
	isoft, isoftSet := xfsQuotaIntArg(args, "isoft")

	mntopts, err := xfsQuotaMountOpts(ctx, conn, mountpoint)
	if err != nil {
		return Result{}, err
	}
	if mntopts == nil {
		return Fail("xfs_quota: " + mountpoint + " is not a mount point or not located on an xfs file system"), nil
	}
	var needAny []string
	switch quotaType {
	case "user":
		needAny = []string{"uquota", "usrquota", "quota", "uqnoenforce", "qnoenforce"}
	case "group":
		needAny = []string{"gquota", "grpquota", "gqnoenforce"}
	case "project":
		needAny = []string{"pquota", "prjquota", "pqnoenforce"}
	}
	if !xfsQuotaAnyOpt(mntopts, needAny) {
		return Fail("xfs_quota: " + mountpoint + " is not mounted with the appropriate quota option for type=" + quotaType), nil
	}

	projectChanged := false
	if quotaType == "project" && name != defaultName {
		res, err := runStatus(ctx, conn, "grep -q "+shellQuote("^"+name+":")+" /etc/projid 2>/dev/null")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("xfs_quota: entry " + name + " has not been defined in /etc/projid"), nil
		}
		out, err := run(ctx, conn, xfsQuotaExec("project "+shellQuote(name), mountpoint))
		if err != nil {
			return Result{}, err
		}
		prjSet := !strings.Contains(out, "is not set")
		switch {
		case state == "present" && !prjSet:
			if _, err := run(ctx, conn, xfsQuotaExec("project -s "+shellQuote(name), mountpoint)); err != nil {
				return Result{}, err
			}
			projectChanged = true
		case state == "absent" && prjSet:
			if _, err := run(ctx, conn, xfsQuotaExec("project -C "+shellQuote(name), mountpoint)); err != nil {
				return Result{}, err
			}
			projectChanged = true
		}
	}

	curBSoft, curBHard, err := xfsQuotaReport(ctx, conn, mountpoint, name, typeArg, "-b", 1024)
	if err != nil {
		return Result{}, err
	}
	curISoft, curIHard, err := xfsQuotaReport(ctx, conn, mountpoint, name, typeArg, "-i", 1)
	if err != nil {
		return Result{}, err
	}
	curRtbSoft, curRtbHard, err := xfsQuotaReport(ctx, conn, mountpoint, name, typeArg, "-r", 1024)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		bhard, bsoft, ihard, isoft, rtbhard, rtbsoft = 0, 0, 0, 0, 0, 0
		bhardSet, bsoftSet, ihardSet, isoftSet, rtbhardSet, rtbsoftSet = true, true, true, true, true, true
	}

	res := Ok("").WithExtra("xfs_quota", map[string]any{
		"bsoft": curBSoft, "bhard": curBHard,
		"isoft": curISoft, "ihard": curIHard,
		"rtbsoft": curRtbSoft, "rtbhard": curRtbHard,
	})

	var limit []string
	addLimit := func(set bool, key string, want, cur int64) {
		if set && want != cur {
			limit = append(limit, key+"="+strconv.FormatInt(want, 10))
			res = res.WithExtra(key, want)
		}
	}
	addLimit(bsoftSet, "bsoft", bsoft, curBSoft)
	addLimit(bhardSet, "bhard", bhard, curBHard)
	addLimit(isoftSet, "isoft", isoft, curISoft)
	addLimit(ihardSet, "ihard", ihard, curIHard)
	addLimit(rtbsoftSet, "rtbsoft", rtbsoft, curRtbSoft)
	addLimit(rtbhardSet, "rtbhard", rtbhard, curRtbHard)

	limitChanged := false
	if len(limit) > 0 {
		var sub string
		if name == defaultName {
			sub = "limit " + typeArg + " -d " + strings.Join(limit, " ")
		} else {
			sub = "limit " + typeArg + " " + strings.Join(limit, " ") + " " + name
		}
		if _, err := run(ctx, conn, xfsQuotaExec(sub, mountpoint)); err != nil {
			return Result{}, err
		}
		limitChanged = true
	}

	if !projectChanged && !limitChanged {
		return res, nil
	}
	res.Changed = true
	return res, nil
}

func xfsQuotaExec(subcommand, mountpoint string) string {
	return "xfs_quota -x -c " + shellQuote(subcommand) + " " + shellQuote(mountpoint)
}

func xfsQuotaSizeArg(args map[string]any, key string) (int64, bool, error) {
	v, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	s, _ := v.(string)
	if s == "" {
		return 0, false, nil
	}
	b, err := filesizeParseBytes(s, 1)
	if err != nil {
		return 0, false, errArg("xfs_quota: %s: %v", key, err)
	}
	return b, true, nil
}

func xfsQuotaIntArg(args map[string]any, key string) (int64, bool) {
	if _, ok := args[key]; !ok {
		return 0, false
	}
	return int64(argInt(args, key, 0)), true
}

// xfsQuotaMountOpts reads /proc/mounts on the target and returns the
// mount options for mountpoint if it is mounted there with fstype
// "xfs", or nil if it is not mounted, or not xfs.
func xfsQuotaMountOpts(ctx context.Context, conn remoteexec.Connection, mountpoint string) ([]string, error) {
	cmd := "awk -v mp=" + shellQuote(mountpoint) + " '$2 == mp && $3 == \"xfs\" {print $4}' /proc/mounts"
	out, err := run(ctx, conn, cmd)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, ","), nil
}

func xfsQuotaAnyOpt(opts []string, want []string) bool {
	for _, o := range opts {
		for _, w := range want {
			if o == w {
				return true
			}
		}
	}
	return false
}

// xfsQuotaReport reads the current soft/hard limit for name via
// `xfs_quota -x -c 'report <typeArg> <usedArg>' mountpoint`, returning
// (0, 0) if no matching row is found (an unset quota), matching real
// xfs_quota's own "current_* if not None else 0" fallback.
func xfsQuotaReport(ctx context.Context, conn remoteexec.Connection, mountpoint, name, typeArg, usedArg string, factor int64) (soft, hard int64, err error) {
	out, err := run(ctx, conn, xfsQuotaExec("report "+typeArg+" "+usedArg, mountpoint))
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 3 && fields[0] == name {
			s, errS := strconv.ParseInt(fields[2], 10, 64)
			h, errH := strconv.ParseInt(fields[3], 10, 64)
			if errS == nil && errH == nil {
				return s * factor, h * factor, nil
			}
		}
	}
	return 0, 0, nil
}
