package modules

import (
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// haproxySocketCmd sends one command line to HAProxy's stats socket and
// returns its raw text reply, matching real haproxy's own `execute()`
// (a raw AF_UNIX socket dial + write + read-to-EOF). This port's
// Connection has no raw-socket primitive (Exec/Put/Fetch/Remove/
// TempPath/Close are its whole surface — see module.go's own doc
// comment), so it substitutes shelling out to `socat`, piping cmd in
// over stdin and the socket's reply back out over stdout:
// `socat - UNIX-CONNECT:<socket>` — chosen over real haproxy's own
// documented `nc` dependency (its NOTES section: "Depends on netcat
// (`nc`) being available") because a plain BSD/GNU `nc -U` does not
// reliably half-close its write side after stdin EOF the way this
// exchange needs (HAProxy's stats socket replies once and then closes
// the connection itself, so a strict read-to-EOF is safe either way in
// practice, but `socat`'s UNIX-CONNECT address type is the more
// consistently available modern equivalent across the distros this
// batch targets).
func haproxySocketCmd(ctx context.Context, conn remoteexec.Connection, socket, cmd string) (string, error) {
	full := "socat - UNIX-CONNECT:" + shellQuote(socket)
	res, err := conn.Exec(ctx, full, strings.NewReader(cmd+"\n"))
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", fmt.Errorf("haproxy: talking to stats socket %s: %s", socket, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// moduleHaproxy implements Ansible's `haproxy` (community.general)
// module: enables, disables, or drains one backend server (or every
// server matching `host` across every backend) via HAProxy's stats
// socket text protocol — see haproxySocketCmd's own doc comment for why
// this port substitutes `socat` for real haproxy's own raw AF_UNIX
// socket dial.
//
// Args: host (required) — the backend server name; backend — the pool
// name; when unset, this port auto-discovers every pool via `show stat`
// and applies the action to `host` in each one it is found in, matching
// real haproxy's own discover_all_backends; socket (default
// /var/run/haproxy.sock); state (required: enabled|disabled|drain);
// weight — `set weight <pxname>/<svname> <weight>`, applied only for
// state=enabled; agent/health (bool) — also `enable|disable
// agent|health <pxname>/<svname>`; shutdown_sessions (bool) — for
// state=disabled (non-drain), also `shutdown sessions server
// <pxname>/<svname>`, overridden by state=drain per real haproxy's own
// documented note; fail_on_not_found (bool, default false) — fail if
// `host` isn't found in a targeted backend, matching real haproxy's own
// execute_for_backends; wait/wait_interval (default 5)/wait_retries
// (default 25) — poll `show stat` for the expected status (UP/MAINT/
// DRAIN) after acting, matching real haproxy's own wait_until_status.
//
// state=drain issues `set server <pxname>/<svname> state drain` only if
// `show info`'s own reported HAProxy version is >= 1.5 (state=drain is
// silently a no-op below that, matching real haproxy's own documented
// version gate exactly — not a Failed result, since real haproxy itself
// treats it the same way).
//
// Extra["state_before"]/Extra["state_after"] mirror real haproxy's own
// return values (each backend server's status/weight/scur, from `show
// stat`, captured before and after acting); Changed is true if they
// differ, matching real haproxy's own act()'s own diff-based Changed
// determination (the same technique pacemaker_cluster.go's own
// modulePacemakerCluster uses for its `out` before/after diff).
//
// Deviation from real haproxy: check_mode (real haproxy's own
// attributes declare check_mode support: none, i.e. real haproxy always
// actually issues its socket commands even under Ansible's own
// --check) needs no reproduction here since this port has no check-mode
// concept at all (see module.go's own doc comment).
func moduleHaproxy(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	host, err := requireString(args, "host")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "enabled" && state != "disabled" && state != "drain" {
		return Result{}, errArg("haproxy: state must be one of enabled, disabled, drain, got %q", state)
	}
	socket := argString(args, "socket", "/var/run/haproxy.sock")
	backend := argString(args, "backend", "")
	weight := argString(args, "weight", "")
	agent := argBool(args, "agent", false)
	health := argBool(args, "health", false)
	shutdownSessions := argBool(args, "shutdown_sessions", false)
	failOnNotFound := argBool(args, "fail_on_not_found", false)
	wait := argBool(args, "wait", false)
	waitInterval := argInt(args, "wait_interval", 5)
	waitRetries := argInt(args, "wait_retries", 25)

	stateBefore, err := haproxyStateFor(ctx, conn, socket, backend, host)
	if err != nil {
		return Result{}, err
	}

	var backends []string
	if backend != "" {
		backends = []string{backend}
	} else {
		backends, err = haproxyDiscoverBackends(ctx, conn, socket)
		if err != nil {
			return Result{}, err
		}
	}

	for _, pxname := range backends {
		states, err := haproxyStateFor(ctx, conn, socket, pxname, host)
		if err != nil {
			return Result{}, err
		}
		if len(states) == 0 {
			if failOnNotFound {
				return Fail(fmt.Sprintf("haproxy: the specified backend '%s/%s' was not found!", pxname, host)), nil
			}
			continue
		}

		var cmd string
		var wantStatus string
		switch state {
		case "enabled":
			cmd = fmt.Sprintf("get weight %s/%s; enable server %s/%s", pxname, host, pxname, host)
			if agent {
				cmd += fmt.Sprintf("; enable agent %s/%s", pxname, host)
			}
			if health {
				cmd += fmt.Sprintf("; enable health %s/%s", pxname, host)
			}
			if weight != "" {
				cmd += fmt.Sprintf("; set weight %s/%s %s", pxname, host, weight)
			}
			wantStatus = "UP"

		case "disabled":
			cmd = fmt.Sprintf("get weight %s/%s", pxname, host)
			if agent {
				cmd += fmt.Sprintf("; disable agent %s/%s", pxname, host)
			}
			if health {
				cmd += fmt.Sprintf("; disable health %s/%s", pxname, host)
			}
			cmd += fmt.Sprintf("; disable server %s/%s", pxname, host)
			if shutdownSessions {
				cmd += fmt.Sprintf("; shutdown sessions server %s/%s", pxname, host)
			}
			wantStatus = "MAINT"

		case "drain":
			version, err := haproxyVersion(ctx, conn, socket)
			if err != nil {
				return Result{}, err
			}
			if version[0] < 1 || (version[0] == 1 && version[1] < 5) {
				continue // below 1.5: real haproxy silently skips drain too
			}
			cmd = fmt.Sprintf("set server %s/%s state drain", pxname, host)
			wantStatus = "DRAIN"
		}

		if _, err := haproxySocketCmd(ctx, conn, socket, cmd); err != nil {
			return Result{}, err
		}

		if wait && !(wantStatus == "DRAIN" && states[0].status == "DOWN") {
			if err := haproxyWaitUntilStatus(ctx, conn, socket, pxname, host, wantStatus, waitInterval, waitRetries); err != nil {
				return Fail(err.Error()), nil
			}
		}
	}

	stateAfter, err := haproxyStateFor(ctx, conn, socket, backend, host)
	if err != nil {
		return Result{}, err
	}

	res := Result{Changed: !haproxyStatesEqual(stateBefore, stateAfter)}
	res = res.WithExtra("state_before", haproxyStatesToAny(stateBefore))
	res = res.WithExtra("state_after", haproxyStatesToAny(stateAfter))
	return res, nil
}

type haproxyServerState struct {
	pxname, status, weight, scur string
}

// haproxyStateFor runs `show stat` and returns every row matching
// svname=host (and pxname, if given), matching real haproxy's own
// get_state_for.
func haproxyStateFor(ctx context.Context, conn remoteexec.Connection, socket, pxname, svname string) ([]haproxyServerState, error) {
	out, err := haproxySocketCmd(ctx, conn, socket, "show stat")
	if err != nil {
		return nil, err
	}
	rows, header, err := haproxyParseCSV(out)
	if err != nil {
		return nil, err
	}
	pxIdx, svIdx, statusIdx, weightIdx, scurIdx := haproxyColIdx(header, "pxname"), haproxyColIdx(header, "svname"),
		haproxyColIdx(header, "status"), haproxyColIdx(header, "weight"), haproxyColIdx(header, "scur")
	var states []haproxyServerState
	for _, row := range rows {
		if svIdx < 0 || svIdx >= len(row) || row[svIdx] != svname {
			continue
		}
		if pxname != "" && (pxIdx < 0 || pxIdx >= len(row) || row[pxIdx] != pxname) {
			continue
		}
		s := haproxyServerState{}
		if pxIdx >= 0 && pxIdx < len(row) {
			s.pxname = row[pxIdx]
		}
		if statusIdx >= 0 && statusIdx < len(row) {
			s.status = row[statusIdx]
		}
		if weightIdx >= 0 && weightIdx < len(row) {
			s.weight = row[weightIdx]
		}
		if scurIdx >= 0 && scurIdx < len(row) {
			s.scur = row[scurIdx]
		}
		states = append(states, s)
	}
	return states, nil
}

// haproxyDiscoverBackends returns every pxname with an svname=BACKEND
// row in `show stat`, matching real haproxy's own discover_all_backends.
func haproxyDiscoverBackends(ctx context.Context, conn remoteexec.Connection, socket string) ([]string, error) {
	out, err := haproxySocketCmd(ctx, conn, socket, "show stat")
	if err != nil {
		return nil, err
	}
	rows, header, err := haproxyParseCSV(out)
	if err != nil {
		return nil, err
	}
	pxIdx, svIdx := haproxyColIdx(header, "pxname"), haproxyColIdx(header, "svname")
	var backends []string
	for _, row := range rows {
		if svIdx >= 0 && svIdx < len(row) && row[svIdx] == "BACKEND" && pxIdx >= 0 && pxIdx < len(row) {
			backends = append(backends, row[pxIdx])
		}
	}
	return backends, nil
}

// haproxyParseCSV parses `show stat`'s own CSV reply, whose header line
// begins with "# " (stripped here, matching real haproxy's own
// `.lstrip("# ")` before csv.DictReader).
func haproxyParseCSV(out string) (rows [][]string, header []string, err error) {
	trimmed := strings.TrimLeft(strings.TrimSpace(out), "# ")
	r := csv.NewReader(strings.NewReader(trimmed))
	r.FieldsPerRecord = -1
	all, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("haproxy: parsing 'show stat' output: %w", err)
	}
	if len(all) == 0 {
		return nil, nil, nil
	}
	return all[1:], all[0], nil
}

func haproxyColIdx(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

// haproxyVersion parses `show info`'s own "Version: X.Y.Z" line into
// [major, minor], matching real haproxy's own discover_version.
func haproxyVersion(ctx context.Context, conn remoteexec.Connection, socket string) ([2]int, error) {
	out, err := haproxySocketCmd(ctx, conn, socket, "show info")
	if err != nil {
		return [2]int{}, err
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "Version:") {
			continue
		}
		_, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
		if len(parts) < 2 {
			continue
		}
		major, err1 := strconv.Atoi(parts[0])
		minor, err2 := strconv.Atoi(parts[1])
		if err1 == nil && err2 == nil {
			return [2]int{major, minor}, nil
		}
	}
	return [2]int{}, nil
}

// haproxyWaitUntilStatus polls `show stat` up to waitRetries times
// (sleeping waitInterval seconds between checks) for svname's status to
// contain wantStatus, matching real haproxy's own wait_until_status
// (including its own substring match, needed for a tracked server's
// "MAINT (via pxname/svname)" status text).
func haproxyWaitUntilStatus(ctx context.Context, conn remoteexec.Connection, socket, pxname, svname, wantStatus string, interval, retries int) error {
	for i := 1; i < retries; i++ {
		states, err := haproxyStateFor(ctx, conn, socket, pxname, svname)
		if err != nil {
			return err
		}
		if len(states) > 0 && strings.Contains(states[0].status, wantStatus) {
			return nil
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
	return fmt.Errorf("haproxy: server %s/%s not status '%s' after %d retries. Aborting.", pxname, svname, wantStatus, retries)
}

func haproxyStatesEqual(a, b []haproxyServerState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func haproxyStatesToAny(states []haproxyServerState) []map[string]any {
	out := make([]map[string]any, len(states))
	for i, s := range states {
		out[i] = map[string]any{"status": s.status, "weight": s.weight, "scur": s.scur}
	}
	return out
}
