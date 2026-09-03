package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacemakerCluster implements Ansible's `pacemaker_cluster`
// module: starts/stops/manages a Pacemaker cluster's overall state via
// the `pcs` CLI (real pacemaker_cluster's own module_utils, _pacemaker,
// wraps `pcs` too — never `crm`; some distros ship `crm` instead, but
// this port matches what real pacemaker_cluster actually requires).
//
// Args: state (required: cleanup|offline|online|restart|maintenance|
// unmaintenance); name (string, aliased node) — a specific node to
// target; "" (the default) means the whole cluster; timeout (int,
// default 300) — passed to `pcs cluster start/stop` as `--wait=N`;
// force (bool, default true) — accepted for argument-shape parity with
// real pacemaker_cluster, but has NO effect: inspecting real
// pacemaker_cluster's own source (community.general's
// plugins/modules/pacemaker_cluster.py) shows `force` is declared in
// its argument_spec but never actually passed to any of its `pcs`
// invocations — this port faithfully reproduces that apparent dead
// argument rather than inventing a `--force` flag real pacemaker_cluster
// itself never sends.
//
// Changed is determined the same way real pacemaker_cluster's own
// StateModuleHelper machinery determines it: by literally diffing the
// cluster's status text (`pcs cluster status`, or `pcs property config
// maintenance-mode` for the maintenance states, or `pcs resource status
// [name]` for cleanup) captured BEFORE and AFTER the action — not by
// reasoning about which individual command ran. The post-action text is
// returned as Extra["out"], matching real pacemaker_cluster's own `out`
// return value.
//
// state=cleanup is deprecated by real pacemaker_cluster itself in favor
// of the pacemaker_resource module; this port implements it anyway
// (community.general still ships it) but does not attempt to model the
// deprecation warning itself, since this package's Result has no
// warnings channel (see pip_package_info.go's identical note about the
// same gap).
func modulePacemakerCluster(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	switch state {
	case "cleanup", "offline", "online", "restart", "maintenance", "unmaintenance":
	default:
		return Result{}, errArg("pacemaker_cluster: state must be one of cleanup, offline, online, restart, maintenance, unmaintenance, got %q", state)
	}
	name := argString(args, "name", argString(args, "node", ""))
	timeout := argInt(args, "timeout", 300)
	// force: accepted, no effect — see doc comment above.

	getCmd := pacemakerClusterGetCmd(state, name)
	before, err := pacemakerStatusText(ctx, conn, getCmd)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "offline":
		cmd := pacemakerStartStopCmd("stop", name, timeout)
		if res, err := pacemakerStep(ctx, conn, cmd, "not currently running"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "online":
		if !pacemakerClusterRunning(before, name) {
			cmd := pacemakerStartStopCmd("start", name, timeout)
			if res, err := pacemakerStep(ctx, conn, cmd, "currently running"); err != nil {
				return Result{}, err
			} else if res.Failed {
				return res, nil
			}
		}
		if res, err := pacemakerClearMaintenance(ctx, conn); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "restart":
		stopCmd := pacemakerStartStopCmd("stop", name, timeout)
		if res, err := pacemakerStep(ctx, conn, stopCmd, "not currently running"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}
		startCmd := pacemakerStartStopCmd("start", name, timeout)
		if res, err := pacemakerStep(ctx, conn, startCmd, "not currently running"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}
		if res, err := pacemakerClearMaintenance(ctx, conn); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "maintenance":
		if res, err := pacemakerStep(ctx, conn, "pcs property set maintenance-mode=true", "Fail"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "unmaintenance":
		if res, err := pacemakerStep(ctx, conn, "pcs property set maintenance-mode=false", "Fail"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}

	case "cleanup":
		cmd := "pcs resource cleanup"
		if name != "" {
			cmd += " " + shellQuote(name)
		}
		if res, err := pacemakerStep(ctx, conn, cmd, "Fail"); err != nil {
			return Result{}, err
		} else if res.Failed {
			return res, nil
		}
	}

	after, err := pacemakerStatusText(ctx, conn, getCmd)
	if err != nil {
		return Result{}, err
	}
	res := Result{Changed: before != after}
	res = res.WithExtra("out", after)
	return res, nil
}

func pacemakerClusterGetCmd(state, name string) string {
	switch state {
	case "maintenance", "unmaintenance":
		return "pcs property config maintenance-mode"
	case "cleanup":
		c := "pcs resource status"
		if name != "" {
			c += " " + shellQuote(name)
		}
		return c
	default:
		c := "pcs cluster status"
		if name != "" {
			c += " " + shellQuote(name)
		}
		return c
	}
}

func pacemakerStartStopCmd(verb, name string, timeout int) string {
	c := "pcs cluster " + verb
	if name != "" {
		c += " " + shellQuote(name)
	} else {
		c += " --all"
	}
	c += " --wait=" + strconv.Itoa(timeout)
	return c
}

// pacemakerStatusText runs cmd and returns its trimmed stdout, treating
// a non-zero exit the same way real pacemaker_cluster's own `_get()`
// does: not an error, just an empty/absent status (e.g. "pcs cluster
// status" exits non-zero with "Error: cluster is not currently running"
// when the cluster is down).
func pacemakerStatusText(ctx context.Context, conn remoteexec.Connection, cmd string) (string, error) {
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// pacemakerStep runs cmd, treating a non-zero exit as a normal
// Result{Failed:true} UNLESS stderr contains ignoreErr — matching real
// pacemaker_cluster/pacemaker_resource's own `_process_command_output`
// tolerance pattern (each real call site names its own "this specific
// error text means nothing actually needs to change" substring).
func pacemakerStep(ctx context.Context, conn remoteexec.Connection, cmd, ignoreErr string) (Result, error) {
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 && res.Stderr != "" && !strings.Contains(res.Stderr, ignoreErr) {
		return Fail(fmt.Sprintf("pcs failed with error (rc=%d): %s", res.RC, strings.TrimSpace(res.Stderr))), nil
	}
	return Ok(""), nil
}

var pacemakerMaintenanceTrueRe = regexp.MustCompile(`(?i)maintenance-mode.*true`)

// pacemakerMaintenanceOn reports whether the cluster's maintenance-mode
// property is currently true, via `pcs property config` — matching
// real _pacemaker.py's get_pacemaker_maintenance_mode.
func pacemakerMaintenanceOn(ctx context.Context, conn remoteexec.Connection) (bool, error) {
	res, err := runStatus(ctx, conn, "pcs property config")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if pacemakerMaintenanceTrueRe.MatchString(line) {
			return true, nil
		}
	}
	return false, nil
}

// pacemakerClearMaintenance turns maintenance-mode off if it is
// currently on, matching real pacemaker_cluster's own state_online/
// state_restart fixup step.
func pacemakerClearMaintenance(ctx context.Context, conn remoteexec.Connection) (Result, error) {
	on, err := pacemakerMaintenanceOn(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if !on {
		return Ok(""), nil
	}
	return pacemakerStep(ctx, conn, "pcs property set maintenance-mode=false", "Fail")
}

// pacemakerClusterRunning reports whether the cluster (or, if name is
// given, that specific node) is up, parsing `pcs cluster status`'s
// output the same way real pacemaker_cluster's own _is_cluster_running
// does: empty output or a "not currently running" substring means
// down; for a specific node, a "* Node <name>:" line containing
// "(offline)" (case-insensitively) means that node is down.
func pacemakerClusterRunning(status, name string) bool {
	if status == "" || strings.Contains(status, "not currently running") {
		return false
	}
	if name == "" {
		return true
	}
	prefix := "* Node " + name + ":"
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) && strings.Contains(strings.ToLower(line), "(offline)") {
			return false
		}
	}
	return true
}
