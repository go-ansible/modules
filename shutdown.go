package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// shutdownDefaultSearchPaths mirrors real shutdown's own default
// search_paths.
var shutdownDefaultSearchPaths = []string{"/sbin", "/usr/sbin", "/usr/local/sbin"}

// moduleShutdown implements Ansible's `shutdown` (community.general)
// module: shuts down the target.
//
// Args: delay (int, default 0, seconds) — passed to the shutdown
// command after real shutdown's own Linux conversion to whole minutes
// (rounded down; under 60s becomes 0); msg (string, default "Shut down
// initiated by Ansible"); search_paths ([]string, default [/sbin,
// /usr/sbin, /usr/local/sbin]) — ONLY these paths are searched for a
// `shutdown` binary, matching real shutdown's own doc ("PATH is ignored
// on the remote node").
//
// Mechanism, matching real shutdown's own corresponding action plugin
// (plugins/action/shutdown.py): search search_paths in order for an
// executable `shutdown`; if none is found, fall back to searching
// [/bin, /usr/bin] for `systemctl` and run `<path>/systemctl poweroff`
// (msg/delay are NOT applied in this fallback, exactly matching real
// shutdown's own documented NOTES); if `shutdown` IS found, run it as
// `<path>/shutdown -h <delay//60> <msg>`.
//
// Simplification vs real shutdown: real shutdown's own action plugin
// gathers ansible_distribution facts first and picks a DISTRO-SPECIFIC
// argument format (Alpine: bare `poweroff`; FreeBSD/Solaris: seconds
// instead of minutes; void/macOS/OpenBSD: `-h +N "msg"`; AIX: `-Fh`;
// see its own SHUTDOWN_COMMAND_ARGS table) — this port has no
// distribution-detection step of its own to key that off, so it always
// uses the generic Linux form (`-h <mins> <msg>`), a documented
// behavioral gap on any non-Linux target.
//
// A shutdown command that severs the connection mid-command is expected
// (that's what a successful shutdown does): if `conn.Exec` itself
// returns a Go error, this port treats that optimistically as a
// successful shutdown — matching real shutdown's own action plugin,
// which explicitly catches an AnsibleConnectionFailure the same way. A
// clean return with a non-zero exit code is treated as a real failure.
func moduleShutdown(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	delay := argInt(args, "delay", 0)
	if delay < 0 {
		delay = 0
	}
	msg := argString(args, "msg", "Shut down initiated by Ansible")
	searchPaths := argStringList(args, "search_paths")
	if len(searchPaths) == 0 {
		searchPaths = shutdownDefaultSearchPaths
	}

	shutdownBin, err := shutdownFindBin(ctx, conn, searchPaths, "shutdown")
	if err != nil {
		return Result{}, err
	}

	var cmd string
	if shutdownBin == "" {
		systemctl, err := shutdownFindBin(ctx, conn, []string{"/bin", "/usr/bin"}, "systemctl")
		if err != nil {
			return Result{}, err
		}
		if systemctl == "" {
			return Fail(fmt.Sprintf("Could not find command \"shutdown\" in search paths: %v or systemctl "+
				"command in search paths: [/bin /usr/bin], unable to shutdown.", searchPaths)), nil
		}
		cmd = systemctl + " poweroff"
	} else {
		delayMin := delay / 60
		cmd = fmt.Sprintf("%s -h %d %s", shutdownBin, delayMin, shellQuote(msg))
	}

	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		// The connection dying mid-command is the expected shape of a
		// successful shutdown; see the doc comment above.
		return Changed("machine is shutting down").WithExtra("shutdown", true).WithExtra("shutdown_command", cmd), nil
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("Shutdown command failed. Error was %s, %s",
			strings.TrimSpace(res.Stdout), strings.TrimSpace(res.Stderr))).WithExtra("shutdown", false), nil
	}
	return Changed("machine is shutting down").WithExtra("shutdown", true).WithExtra("shutdown_command", cmd), nil
}

// shutdownFindBin returns the first "<path>/<bin>" for which `test -x`
// succeeds, or "" if none of searchPaths has an executable bin.
func shutdownFindBin(ctx context.Context, conn remoteexec.Connection, searchPaths []string, bin string) (string, error) {
	for _, p := range searchPaths {
		full := strings.TrimRight(p, "/") + "/" + bin
		res, err := runStatus(ctx, conn, "test -x "+shellQuote(full))
		if err != nil {
			return "", err
		}
		if res.RC == 0 {
			return full, nil
		}
	}
	return "", nil
}
