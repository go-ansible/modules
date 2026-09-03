package modules

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSeport implements Ansible's `seport` module: manages SELinux
// network port type definitions, similar to `semanage port`.
//
// Args: ports ([]string or comma-separated string, required) — ports or
// port ranges (e.g. "8888", "10000-10100"); proto (tcp|udp|dccp|sctp,
// required); setype (string, required); state (present|absent, default
// "present"); reload (bool, default true); local (bool, default false)
// — restrict the idempotency check to LOCAL customizations only
// (semanage's `-C` flag), matching real seport's own
// `get_all_by_type(locallist=local)`; ignore_selinux_state (bool,
// default false, accepted as a no-op — see sefcontext.go's identical
// note).
//
// Real seport is implemented entirely against the Python `seobject`
// binding (seobject.portRecords), never a CLI. This port composes the
// `semanage port` command instead, the tool seobject itself wraps:
// `-a`/`-m` to add or modify (add when the exact port/range has no
// existing type definition at all, modify when it already has one —
// mirroring real seport's own semanage_port_get_type add-vs-modify
// branch), `-d` to delete, `-p` for proto, `-t` for setype, `-r` for
// selevel, `-N` to suppress the post-commit reload.
//
// Idempotency for state=present mirrors real seport's own
// "already-covered" semantics exactly: a requested port or range is
// left alone if it already falls entirely within an EXISTING range
// already assigned to the same (setype, proto) — not just on an exact
// string match — matching real seport's `_port_is_covered`. For
// state=absent, real seport only removes an EXACT existing entry
// (string-equal port/range assigned to that exact (setype, proto)), and
// this port matches that: a sub-range of a broader existing range is
// NOT removed, exactly like real seport.
//
// `semanage port -l` is parsed into rows of "<setype>  <proto>
// <port-or-range>[, <port-or-range> ...]" (columns separated by runs of
// two or more spaces, entries within the last column separated by ", ").
// Like this batch's other `semanage`-based modules, this exact shape is
// this port's own assumption, not verified against a live SELinux
// system in this sandbox — a disclosed limitation.
func moduleSeport(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	ports := seportPorts(args)
	if len(ports) == 0 {
		return Result{}, errArg("seport: missing required argument: ports")
	}
	proto := argString(args, "proto", "")
	switch proto {
	case "tcp", "udp", "dccp", "sctp":
	default:
		return Result{}, errArg("seport: proto must be one of tcp, udp, dccp, sctp, got %q", proto)
	}
	setype, err := requireString(args, "setype")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("seport: state must be present or absent, got %q", state)
	}
	reload := argBool(args, "reload", true)
	local := argBool(args, "local", false)

	listCmd := "semanage port -l"
	if local {
		listCmd = "semanage port -C -l"
	}
	listOut, err := run(ctx, conn, listCmd)
	if err != nil {
		return Result{}, err
	}
	entries, err := parseSeportList(listOut)
	if err != nil {
		return Result{}, errArg("seport: %v", err)
	}

	changed := false
	switch state {
	case "present":
		for _, p := range ports {
			low, high, err := seportParseRange(p)
			if err != nil {
				return Result{}, errArg("seport: %v", err)
			}
			if seportCovered(entries, setype, proto, low, high) {
				continue
			}
			action := "a"
			if seportExactMatchAnyType(entries, proto, low, high) {
				action = "m"
			}
			cmd := seportCmd(action, proto, setype, "", p, reload)
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true
		}
	case "absent":
		for _, p := range ports {
			if seportExactMatch(entries, setype, proto, p) {
				cmd := seportCmd("d", proto, "", "", p, reload)
				if _, err := run(ctx, conn, cmd); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}
	}

	res := Result{Changed: changed}
	portsAny := make([]any, len(ports))
	for i, p := range ports {
		portsAny[i] = p
	}
	res = res.WithExtra("ports", portsAny).WithExtra("proto", proto).
		WithExtra("setype", setype).WithExtra("state", state)
	return res, nil
}

func seportPorts(args map[string]any) []string {
	list := argStringList(args, "ports")
	var out []string
	for _, item := range list {
		for _, p := range strings.Split(item, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

type seportEntry struct {
	setype, proto string
	low, high     int
}

var seportCols = regexp.MustCompile(`\s{2,}`)

func parseSeportList(out string) ([]seportEntry, error) {
	var entries []seportEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := seportCols.Split(line, -1)
		if len(fields) < 3 {
			continue
		}
		setype, proto := fields[0], fields[1]
		switch proto {
		case "tcp", "udp", "dccp", "sctp":
		default:
			continue // not a port row (e.g. a heading)
		}
		for _, p := range strings.Split(strings.Join(fields[2:], " "), ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			low, high, err := seportParseRange(p)
			if err != nil {
				continue
			}
			entries = append(entries, seportEntry{setype: setype, proto: proto, low: low, high: high})
		}
	}
	return entries, nil
}

func seportParseRange(port string) (low, high int, err error) {
	before, after, ok := strings.Cut(port, "-")
	low, err = strconv.Atoi(strings.TrimSpace(before))
	if err != nil {
		return 0, 0, errArg("invalid port %q: %v", port, err)
	}
	if !ok {
		return low, low, nil
	}
	high, err = strconv.Atoi(strings.TrimSpace(after))
	if err != nil {
		return 0, 0, errArg("invalid port range %q: %v", port, err)
	}
	return low, high, nil
}

// seportCovered reports whether [low,high] falls entirely within an
// existing entry already assigned to (setype, proto).
func seportCovered(entries []seportEntry, setype, proto string, low, high int) bool {
	for _, e := range entries {
		if e.setype == setype && e.proto == proto && e.low <= low && high <= e.high {
			return true
		}
	}
	return false
}

// seportExactMatchAnyType reports whether [low,high] is an exact
// existing entry for proto, under ANY setype (used to decide add vs
// modify, matching real seport's semanage_port_get_type).
func seportExactMatchAnyType(entries []seportEntry, proto string, low, high int) bool {
	for _, e := range entries {
		if e.proto == proto && e.low == low && e.high == high {
			return true
		}
	}
	return false
}

func seportExactMatch(entries []seportEntry, setype, proto, port string) bool {
	low, high, err := seportParseRange(port)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.setype == setype && e.proto == proto && e.low == low && e.high == high {
			return true
		}
	}
	return false
}

func seportCmd(action, proto, setype, selevel, port string, reload bool) string {
	var b strings.Builder
	b.WriteString("semanage port -")
	b.WriteString(action)
	b.WriteString(" -p ")
	b.WriteString(proto)
	if setype != "" {
		b.WriteString(" -t ")
		b.WriteString(shellQuote(setype))
	}
	if selevel != "" {
		b.WriteString(" -r ")
		b.WriteString(shellQuote(selevel))
	}
	if !reload {
		b.WriteString(" -N")
	}
	b.WriteString(" ")
	b.WriteString(shellQuote(port))
	return b.String()
}
