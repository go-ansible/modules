package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleListenPortsFacts implements Ansible's `listen_ports_facts`
// (community.general) module: gathers read-only facts about TCP/UDP
// listening (or, with include_non_listening, all) sockets into
// Extra["tcp_listen"]/Extra["udp_listen"] by parsing `netstat` or `ss`
// output — read from real listen_ports_facts.py's own parse functions
// (netStatParse/ss_parse), since the exact column layouts handled and
// the pid/name-splitting quirks aren't visible from EXAMPLES/RETURN
// VALUES alone.
//
// Args: command (string, optional — "netstat" or "ss"; unset tries
// netstat then ss, in that (alphabetical) order, matching real
// listen_ports_facts' own `sorted(commands_map)` selection — the first
// one found on PATH wins); include_non_listening (bool, default
// false) — when true, uses `-p -u -n -t -a` instead of the default
// `-p -l -u -n -t`, and additionally returns each entry's `state` and
// `foreign_address` (both otherwise omitted, matching real
// listen_ports_facts' own field-pruning for the default case).
//
// This module requires Linux (checked via `uname -s`, matching real
// listen_ports_facts' own `platform.system() != "Linux"` fail_json).
//
// Each connection's stime/user fields cost real listen_ports_facts a
// separate `ps -o lstart -p <pid>`/`ps -o user -p <pid>` call PER
// CONNECTION; this port does the same (`ps` is queried once per
// connection over conn.Exec), except it CACHES both by pid within one
// module run — a pure optimization (real listen_ports_facts re-runs
// `ps` for every connection even when several share a pid) that cannot
// change the result, since a given pid's start time and owning user
// can't change between two `ps` calls issued moments apart within the
// same module invocation.
//
// Both helper `ps` calls' own header-row handling in real
// listen_ports_facts (skip a line matching a literal string) is
// reproduced here as "keep the LAST output line" instead — real
// listen_ports_facts' own filter compares against the wrong case
// ("started" vs `ps`'s own "STARTED" header) and so never actually
// filters anything on Linux; either way, the loop's own top-to-bottom
// overwrite means the final assignment always ends up being the last
// (data) line regardless, so "last line" is behavior-identical, just
// without carrying the same dead code forward.
func moduleListenPortsFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	includeNonListening := argBool(args, "include_non_listening", false)
	command := argString(args, "command", "")
	if command != "" && command != "netstat" && command != "ss" {
		return Result{}, errArg("listen_ports_facts: command must be netstat or ss, got %q", command)
	}

	osName, err := run(ctx, conn, "uname -s")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(osName) != "Linux" {
		return Fail("listen_ports_facts: this module requires Linux"), nil
	}

	if command == "" {
		for _, c := range []string{"netstat", "ss"} { // sorted order, matching real listen_ports_facts
			res, err := runStatus(ctx, conn, "command -v "+c+" >/dev/null 2>&1")
			if err != nil {
				return Result{}, err
			}
			if res.RC == 0 {
				command = c
				break
			}
		}
		if command == "" {
			return Fail("listen_ports_facts: unable to find any of the supported commands in PATH: netstat, ss"), nil
		}
	} else {
		res, err := runStatus(ctx, conn, "command -v "+command+" >/dev/null 2>&1")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("listen_ports_facts: " + command + " not found in PATH"), nil
		}
	}

	flags := "-p -l -u -n -t"
	if includeNonListening {
		flags = "-p -u -n -t -a"
	}
	out, err := run(ctx, conn, command+" "+flags)
	if err != nil {
		return Result{}, err
	}

	var conns []listenPortsConn
	if command == "netstat" {
		conns = listenPortsParseNetstat(out)
	} else {
		conns, err = listenPortsParseSS(out)
		if err != nil {
			return Fail("listen_ports_facts: " + err.Error()), nil
		}
	}

	tcpListen := []map[string]any{}
	udpListen := []map[string]any{}
	stimeCache := map[int]string{}
	userCache := map[int]string{}
	for _, c := range conns {
		stime, ok := stimeCache[c.pid]
		if !ok {
			stime, err = listenPortsPsField(ctx, conn, "lstart", c.pid)
			if err != nil {
				return Result{}, err
			}
			stimeCache[c.pid] = stime
		}
		user, ok := userCache[c.pid]
		if !ok {
			user, err = listenPortsPsField(ctx, conn, "user", c.pid)
			if err != nil {
				return Result{}, err
			}
			userCache[c.pid] = user
		}

		entry := map[string]any{
			"address":  c.address,
			"name":     c.name,
			"pid":      c.pid,
			"port":     c.port,
			"protocol": c.protocol,
			"stime":    stime,
			"user":     user,
		}
		if includeNonListening {
			entry["state"] = c.state
			entry["foreign_address"] = c.foreignAddress
		}
		switch {
		case strings.HasPrefix(c.protocol, "tcp"):
			tcpListen = append(tcpListen, entry)
		case strings.HasPrefix(c.protocol, "udp"):
			udpListen = append(udpListen, entry)
		}
	}
	return Ok("").WithExtra("tcp_listen", tcpListen).WithExtra("udp_listen", udpListen), nil
}

// listenPortsConn is one parsed connection entry, shared by both
// netstat and ss parsing.
type listenPortsConn struct {
	protocol, state, address, foreignAddress, name string
	port, pid                                      int
}

// listenPortsSplitPidName splits a netstat "PID/Program name" cell
// (e.g. "51/sshd:") into its pid and name; an unparseable or missing
// pid (e.g. "-", from an unprivileged user) yields (0, "").
func listenPortsSplitPidName(pidName string) (pid int, name string) {
	idx := strings.IndexByte(pidName, '/')
	if idx < 0 {
		return 0, ""
	}
	p, err := strconv.Atoi(pidName[:idx])
	if err != nil {
		return 0, ""
	}
	return p, strings.TrimSuffix(pidName[idx+1:], ":")
}

// listenPortsParseNetstat parses `netstat -p -l -u -n -t` (or with -a
// instead of -l) output: one entry per "tcp"/"udp"-prefixed line,
// de-duplicating exact repeats (matching real netStatParse's own `if
// result not in results`).
func listenPortsParseNetstat(raw string) []listenPortsConn {
	var results []listenPortsConn
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "tcp") && !strings.HasPrefix(line, "udp") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		protocol := fields[0]
		localAddr := fields[3]
		foreignAddress := fields[4]
		rest := fields[5:]

		idx := strings.LastIndexByte(localAddr, ':')
		if idx < 0 {
			continue
		}
		address := localAddr[:idx]
		port, err := strconv.Atoi(localAddr[idx+1:])
		if err != nil {
			continue
		}

		var state, pidAndName string
		switch {
		case strings.HasPrefix(protocol, "tcp"):
			protocol = "tcp"
			if len(rest) == 3 || len(rest) == 2 {
				state, pidAndName = rest[0], rest[1]
			}
		case strings.HasPrefix(protocol, "udp"):
			protocol = "udp"
			if len(rest) == 2 {
				pidAndName = rest[0]
			} else if len(rest) == 1 {
				pidAndName = rest[0]
			}
		default:
			continue
		}

		pid, name := listenPortsSplitPidName(pidAndName)
		c := listenPortsConn{
			protocol: protocol, state: state, address: address,
			foreignAddress: foreignAddress, port: port, name: name, pid: pid,
		}
		dup := false
		for _, existing := range results {
			if existing == c {
				dup = true
				break
			}
		}
		if !dup {
			results = append(results, c)
		}
	}
	return results
}

var (
	listenPortsSSConnRe = regexp.MustCompile(`\[?(.+?)\]?:([0-9]+)$`)
	listenPortsSSPidRe  = regexp.MustCompile(`"(.*?)",pid=(\d+)`)
)

// listenPortsParseSS parses `ss -p -l -u -n -t` (or with -a instead of
// -l) output, including its "Netid " header sanity check.
func listenPortsParseSS(raw string) ([]listenPortsConn, error) {
	lines := strings.Split(raw, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "Netid ") {
		return nil, fmt.Errorf("unknown stdout format of `ss`: %s", raw)
	}
	lines = lines[1:]

	var results []listenPortsConn
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells := listenPortsSplitFieldsN(line, 7)
		var protocol, state, localAddrPort, peerAddrPort, process string
		switch len(cells) {
		case 6:
			protocol, state, localAddrPort, peerAddrPort = cells[0], cells[1], cells[4], cells[5]
		case 7:
			protocol, state, localAddrPort, peerAddrPort, process = cells[0], cells[1], cells[4], cells[5], cells[6]
		default:
			return nil, fmt.Errorf("expected `ss` table layout \"Netid, State, Recv-Q, Send-Q, Local Address:Port, "+
				"Peer Address:Port\" and optionally \"Process\", but got something else: %s", line)
		}

		conn := listenPortsSSConnRe.FindStringSubmatch(localAddrPort)
		pids := listenPortsSSPidRe.FindAllStringSubmatch(process, -1)
		if conn == nil {
			continue
		}
		if len(pids) == 0 {
			pids = [][]string{{"", "", "0"}}
		}
		port, _ := strconv.Atoi(conn[2])
		for _, pm := range pids {
			name := pm[1]
			pid, _ := strconv.Atoi(pm[len(pm)-1])
			results = append(results, listenPortsConn{
				protocol: protocol, state: state, address: conn[1],
				foreignAddress: peerAddrPort, port: port, name: name, pid: pid,
			})
		}
	}
	return results, nil
}

// listenPortsSplitFieldsN mimics Python's str.split(None, n-1):
// whitespace-collapsing split into at most n fields, where the last
// field keeps the remainder of the line verbatim (internal whitespace
// included) rather than being split further.
func listenPortsSplitFieldsN(s string, n int) []string {
	var fields []string
	rest := s
	for i := 0; i < n-1; i++ {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return fields
		}
		idx := strings.IndexAny(rest, " \t")
		if idx < 0 {
			fields = append(fields, rest)
			return fields
		}
		fields = append(fields, rest[:idx])
		rest = rest[idx:]
	}
	rest = strings.TrimLeft(rest, " \t")
	if rest != "" {
		fields = append(fields, strings.TrimRight(rest, " \t\r"))
	}
	return fields
}

// listenPortsPsField runs `ps -o <field> -p <pid>` and returns its last
// output line (see moduleListenPortsFacts' own doc comment for why
// "last line" is behavior-identical to real listen_ports_facts' own
// header-skipping loop). A non-zero exit (e.g. pid 0, from an
// unprivileged-user connection with no discoverable owner) yields "".
func listenPortsPsField(ctx context.Context, conn remoteexec.Connection, field string, pid int) (string, error) {
	res, err := runStatus(ctx, conn, "ps -o "+field+" -p "+strconv.Itoa(pid))
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	lines := strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n")
	if len(lines) == 0 {
		return "", nil
	}
	return strings.TrimSpace(lines[len(lines)-1]), nil
}
