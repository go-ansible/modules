package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSysupgrade implements Ansible's `sysupgrade` (community.general)
// module: performs an OpenBSD major-version (or snapshot) upgrade via
// the `sysupgrade` binary — OpenBSD's own release/snapshot upgrade
// tool, distinct from syspatch.go's `syspatch` (binary patches within
// the CURRENT release; sysupgrade instead fetches and stages an
// upgrade to a newer release or snapshot, normally applied on next
// reboot).
//
// Args: snapshot (bool, default false) — apply the latest snapshot
// (`-s`) instead of the latest release (`-r`); force (bool, default
// false) — `-f`, only meaningful with snapshot (matching real
// sysupgrade's own doc: "Force upgrade (for snapshots only)"); this
// port composes `-f` whenever both snapshot and force are set,
// regardless of that documented restriction, exactly as real
// sysupgrade's own module does (it never conditions -f's inclusion on
// anything but snapshot); keep_files (bool, default false) — `-k`,
// keep files under /home/_sysupgrade instead of deleting them after
// upgrade; fetch_only (bool, default true) — `-n`, fetch and verify
// only, do not reboot; installurl (string, optional) — OpenBSD mirror
// URL, passed as sysupgrade's own last positional argument.
//
// A non-zero exit is always a failure. On success, changed is derived
// from sysupgrade's own stdout wording, matching real sysupgrade's own
// module: stdout containing "already on latest snapshot" (case
// insensitive) reports unchanged; stdout containing "upgrade on next
// reboot" reports changed; any other successful output reports
// unchanged (real sysupgrade's own `changed` starts False and is only
// ever set True by the second phrase).
//
// No check_mode support, matching real sysupgrade's own module
// (supports_check_mode=False). This port does not attempt to run
// fetch_only=false (an automatic, forced reboot mid-command) any
// differently than any other invocation — real sysupgrade's own
// EXAMPLES document that combination as expected to error out under
// Ansible regardless, via `ignore_errors: true`.
func moduleSysupgrade(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	snapshot := argBool(args, "snapshot", false)
	force := argBool(args, "force", false)
	keepFiles := argBool(args, "keep_files", false)
	fetchOnly := argBool(args, "fetch_only", true)
	installURL := argString(args, "installurl", "")

	var flags []string
	if snapshot {
		flags = append(flags, "-s")
		if force {
			flags = append(flags, "-f")
		}
	} else {
		flags = append(flags, "-r")
	}
	if keepFiles {
		flags = append(flags, "-k")
	}
	if fetchOnly {
		flags = append(flags, "-n")
	}
	if installURL != "" {
		flags = append(flags, installURL)
	}

	cmd := quoteAll(append([]string{"/usr/sbin/sysupgrade"}, flags...))
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	extra := map[string]any{"rc": res.RC, "stdout": res.Stdout, "stderr": res.Stderr}
	if res.RC != 0 {
		r := Fail(fmt.Sprintf("Command sysupgrade failed rc=%d, out=%s, err=%s", res.RC, res.Stdout, res.Stderr))
		for k, v := range extra {
			r = r.WithExtra(k, v)
		}
		return r, nil
	}

	out := strings.ToLower(res.Stdout)
	changed := false
	if strings.Contains(out, "upgrade on next reboot") {
		changed = true
	}
	r := Result{Changed: changed}
	for k, v := range extra {
		r = r.WithExtra(k, v)
	}
	return r, nil
}
