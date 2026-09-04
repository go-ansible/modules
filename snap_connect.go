package modules

import (
	"context"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSnapConnect implements Ansible's `snap_connect` module:
// connects (or disconnects) a snap interface plug to a slot, via `snap
// connect`/`snap disconnect` — see snap.go/snap_alias.go for this
// port's sibling snap modules.
//
// Args: plug (string, required) — a `<snap>:<plug>` endpoint. slot
// (string, optional) — a `<snap>:<slot>` or `:<slot>` (system slot)
// endpoint; when omitted, snapd resolves a matching slot automatically
// (matching real snap_connect's own documented behavior for both
// connect and disconnect — `slot` is passed through as-is, or omitted
// entirely, never defaulted by this port). state (present|absent,
// default "present").
//
// Idempotency matches real snap_connect's own `_is_connected()`
// exactly: `snap connections` is parsed (skipping its header line) into
// interface/plug/slot triples via the same `^(\S+)\s+(\S+)\s+(\S+)\s+.*$`
// shape real snap_connect's own regex uses, and a connection counts as
// "connected" only when its slot column is not "-" (snapd's own marker
// for an unconnected plug's row) AND the plug matches AND (slot is
// unset OR the slot matches) — used for BOTH state=present (run connect
// if not yet connected) and state=absent (run disconnect if currently
// connected), exactly like real snap_connect's own single
// _is_connected() check reused by both state_present/state_absent.
//
// Extra["snap_connections"] is always the connections list AFTER this
// task ran (re-fetched if anything changed), and Extra["version"] is
// `snap version`'s own key/value output parsed into a map, matching
// real snap_connect's own two RETURN values exactly — including that a
// failed/empty `snap connections`/`snap version` probe (rc != 0) is
// treated as "nothing/no version info", not a module failure, matching
// real snap_connect's own get_version()/`_connections` handling (both
// have `check_rc=False` in real code).
//
// This port has no check_mode/diff_mode support at all (a runtime-
// engine concern outside every module's own Func signature here),
// unlike real snap_connect which supports both.
func moduleSnapConnect(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	plug, err := requireString(args, "plug")
	if err != nil {
		return Result{}, err
	}
	slot := argString(args, "slot", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("snap_connect: state must be present or absent, got %q", state)
	}

	version, err := snapVersionMap(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	connections, err := snapConnectionsList(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	connected := snapIsConnected(connections, plug, slot)

	changed := false
	switch {
	case state == "present" && !connected:
		changed = true
		cmd := "snap connect " + shellQuote(plug)
		if slot != "" {
			cmd += " " + shellQuote(slot)
		}
		res, err := runStatus(ctx, conn, cmd)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("snap_connect: snap connect failed: " + strings.TrimSpace(res.Stderr)), nil
		}
	case state == "absent" && connected:
		changed = true
		cmd := "snap disconnect " + shellQuote(plug)
		if slot != "" {
			cmd += " " + shellQuote(slot)
		}
		res, err := runStatus(ctx, conn, cmd)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("snap_connect: snap disconnect failed: " + strings.TrimSpace(res.Stderr)), nil
		}
	}

	if changed {
		connections, err = snapConnectionsList(ctx, conn)
		if err != nil {
			return Result{}, err
		}
	}

	result := Ok("unchanged")
	if changed {
		result = Changed(plug)
	}
	result = result.WithExtra("snap_connections", connections).WithExtra("version", version)
	return result, nil
}

var snapConnectionLineRE = regexp.MustCompile(`^(\S+)\s+(\S+)\s+(\S+)\s+.*$`)

// snapConnectionsList runs `snap connections` and parses it into
// {interface, plug, slot} maps, matching real snap_connect's own
// _get_connections(): a non-zero exit (rc != 0) is treated as "no
// connections", not a module failure, and the header line is always
// skipped.
func snapConnectionsList(ctx context.Context, conn remoteexec.Connection) ([]map[string]any, error) {
	res, err := runStatus(ctx, conn, "snap connections")
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if res.RC != 0 {
		return out, nil
	}
	lines := strings.Split(res.Stdout, "\n")
	if len(lines) > 0 {
		lines = lines[1:] // skip header
	}
	for _, line := range lines {
		m := snapConnectionLineRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		out = append(out, map[string]any{"interface": m[1], "plug": m[2], "slot": m[3]})
	}
	return out, nil
}

// snapIsConnected matches real snap_connect's own _is_connected():
// true if some connection has slot != "-", plug == plug, and (slot ==
// "" or that connection's slot == slot).
func snapIsConnected(connections []map[string]any, plug, slot string) bool {
	for _, c := range connections {
		if c["slot"] == "-" {
			continue
		}
		if c["plug"] != plug {
			continue
		}
		if slot != "" && c["slot"] != slot {
			continue
		}
		return true
	}
	return false
}

// snapVersionMap runs `snap version` and parses its own key/value
// lines into a map, matching real snap_connect's own get_version():
// `dict(x.split() for x in out.splitlines() if len(x.split()) == 2)` —
// a non-zero exit is treated as "no version info", not a module
// failure.
func snapVersionMap(ctx context.Context, conn remoteexec.Connection) (map[string]any, error) {
	res, err := runStatus(ctx, conn, "snap version")
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if res.RC != 0 {
		return out, nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			out[fields[0]] = fields[1]
		}
	}
	return out, nil
}
