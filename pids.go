package modules

import (
	"context"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePids implements (a subset of) Ansible's `pids`
// (community.general) module: lists the PIDs of processes matching a
// name or a regular-expression pattern — read from real pids.py's own
// PSAdapter/Pids classes (this batch's hard rule: the exact name-vs-
// cmdline[0] and name/exe/cmdline regex-search matching rules are only
// visible there, not EXAMPLES/RETURN VALUES).
//
// Args: name (string) — exact match (case-insensitive); pattern
// (string) — a regular expression; ignore_case (bool, default false,
// only meaningful with pattern). Exactly one of name or pattern is
// required, matching real pids' own required_one_of+mutually_exclusive.
//
// Real pids has no CLI-tool dependency at all: it uses the `psutil`
// Python library directly on the CONTROLLED host, matching each
// process's own `name`/`cmdline`/`exe` attributes. This port has no Go
// process-inspection library and, per this package's architecture,
// only reaches the target through Connection.Exec — so it shells out
// to the target's own `pgrep` (from procps on Linux, or the BSD-
// derived pgrep already present on macOS) instead. This is a genuine,
// documented behavioral narrowing, not just a syntax difference:
//   - name: real pids matches if EITHER the process's own short name
//     OR the first element of its cmdline equals name
//     (case-insensitively) — this port instead runs `pgrep -i -x --
//     <name>`, whose own `-x` flag matches only a process's short name
//     (`comm`, kernel-truncated to 15 characters on Linux) exactly;
//     the cmdline[0] fallback real pids also checks (e.g. a script
//     invoked as `/usr/bin/env python3 foo.py`, whose own comm is
//     "python3" but whose cmdline[0] might differ) is NOT reproduced.
//   - pattern: real pids searches (re.search, not fullmatch) the
//     pattern against name, exe's basename, AND the full cmdline
//     joined with spaces — matching if ANY of the three hit. This port
//     instead runs `pgrep -i? -f -- <pattern>`, whose own `-f` flag
//     matches the pattern (as a POSIX extended regular expression, a
//     close but not identical dialect to Python's re) against the
//     full command line only; a process whose executable basename or
//     short name alone would match but whose full command line would
//     not (a rare case in practice, since the command line almost
//     always contains the executable name too) would be missed here
//     but caught by real pids.
//   - Ordering: real pids returns PIDs in whatever order psutil's own
//     process_iter happens to enumerate them (unspecified, effectively
//     process-table order). This port sorts them numerically ascending
//     (pgrep's own default output order is already numeric-ascending
//     on every platform this was checked against, so this is
//     documented rather than actively enforced by extra sort logic
//     working around pgrep — the explicit sort here is defensive, in
//     case a target's pgrep variant doesn't guarantee that).
//
// pgrep's own exit code 1 ("no processes matched") is treated as an
// empty list, matching real pids' own documented "returns an empty
// list if no process in that name exists" — not a failure. Exit code 2
// (pattern syntax error) is reported as Result{Failed:true}, mirroring
// real pids' own PSAdapterError -> module.fail_json for an invalid
// regular expression. Any other non-zero/non-{0,1,2} exit is treated
// as a hard failure too (pgrep itself missing produces "command not
// found", exit 127, from the shell — also surfaced as Result{Failed:
// true} via the same path, rather than a Go error, since it's the kind
// of "this specific request can't be satisfied on this target" outcome
// this package's own Func doc comment reserves Result{Failed:true} for).
func modulePids(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	_, hasName := args["name"]
	_, hasPattern := args["pattern"]
	if hasName == hasPattern {
		return Result{}, errArg("pids: exactly one of name or pattern is required")
	}
	ignoreCase := argBool(args, "ignore_case", false)

	var cmd string
	if hasName {
		name, err := requireString(args, "name")
		if err != nil {
			return Result{}, err
		}
		cmd = "pgrep -i -x -- " + shellQuote(name)
	} else {
		pattern, err := requireString(args, "pattern")
		if err != nil {
			return Result{}, err
		}
		flags := "-f"
		if ignoreCase {
			flags = "-i -f"
		}
		cmd = "pgrep " + flags + " -- " + shellQuote(pattern)
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	switch res.RC {
	case 0, 1:
		// 0: matches found; 1: no processes matched (empty list).
	case 2:
		return Fail("pids: invalid pattern: " + strings.TrimSpace(res.Stderr)), nil
	default:
		return Fail("pids: pgrep failed: " + strings.TrimSpace(res.Stderr)), nil
	}

	pids := []int{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		pids = append(pids, n)
	}
	sort.Ints(pids)

	return Ok("").WithExtra("pids", pids), nil
}
