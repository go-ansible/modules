package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRiak implements Ansible's `riak` (community.general) module:
// joins a Riak KV node to a cluster, checks/commits a staged cluster
// plan, or waits for handoffs/ring convergence/a service to come up —
// via the `riak-admin` CLI (falling back to the newer `riak admin`
// subcommand form when `riak-admin` isn't on the target's PATH,
// matching real riak.py's own `module.get_bin_path("riak-admin")` ->
// `[riak_bin, "admin"]` fallback exactly) plus a `curl` call against
// Riak's own HTTP `/stats` endpoint (real riak.py's own `fetch_url`
// call, substituted the same way consul_session.go's own doc comment
// explains for the same reason: this port has no HTTP client wired
// into remoteexec.Connection, so the request is issued as `curl` run
// ON THE TARGET via conn.Exec instead).
//
// Args: command (choices: ping|kv_test|join|plan|commit, optional —
// omitted means only the base node_name/nodes/ring_size/version facts
// and any wait_for_* actions below run, no cluster command at all);
// target_node (default riak@127.0.0.1) — used by ping/join; http_conn
// (default 127.0.0.1:8098) — Riak's own HTTP query listener, queried
// for /stats; wait_for_handoffs (int seconds, default 0); wait_for_ring
// (int seconds, default 0); wait_for_service (choices: kv, optional);
// validate_certs (bool, default true) — curl's own `-k` when false.
//
// Deviation — config_dir is accepted (default /etc/riak) for
// argument-shape compatibility but has NO EFFECT: verified directly
// against real riak.py's own source, `config_dir` is declared in its
// argument_spec but never once read from module.params anywhere in
// main() — a real, harmless dead argument in the upstream module,
// preserved as a no-op here rather than invented a use for it.
//
// Every run first fetches Riak's own /stats (retrying every 5 seconds
// for up to 120 seconds if the target isn't answering yet, matching
// real riak.py's own hardcoded retry loop exactly) to populate
// Extra["node_name"]/["nodes"]/["ring_size"] from its own "nodename"/
// "ring_members"/"ring_creation_size" fields, and runs `riak version`
// for Extra["version"].
//
// command=ping: `riak ping <target_node>`; success sets
// Extra["ping"] to its own output; failure is a Fail with that output
// as the message, matching real riak.py's own `module.fail_json(msg=out)`
// (not stderr — real riak.py passes its OWN stdout as msg here, an odd
// but verified choice this port preserves).
//
// command=kv_test: `<riak-admin> test`; same success/failure shape,
// Extra["kv_test"].
//
// command=join: idempotent — if the node's own name already appears
// more than once in `nodes` (Riak's own `/stats` "ring_members" list)
// AND that list has more than one member, this port treats the node
// as already joined/staged (Extra["join"] = "Node is already in
// cluster or staged to be in cluster.", Changed=false) without running
// anything, matching real riak.py's own `nodes.count(node_name) == 1
// and len(nodes) > 1` check EXACTLY (note: `== 1`, not `> 1` — a
// single self-membership entry alongside at least one other node is
// what upstream treats as "already there"; this port replicates that
// literal comparison rather than a more intuitive `>= 1`, since it is
// what the real module actually checks). Otherwise `<riak-admin>
// cluster join <target_node>`; success is Changed=true with
// Extra["join"] set to its output.
//
// command=plan: `<riak-admin> cluster plan`; Extra["plan"] = output;
// Changed=true only if that output contains the literal substring
// "Staged Changes", matching real riak.py's own check.
//
// command=commit: `<riak-admin> cluster commit`; Changed=true
// unconditionally on success, Extra["commit"] = output.
//
// wait_for_handoffs (seconds > 0): polls `<riak-admin> transfers`
// every 10 seconds until its output contains "No transfers active"
// (Extra["handoffs"] set to that same literal string), or Fails with
// "Timeout waiting for handoffs." once wait_for_handoffs seconds have
// elapsed — matching real riak.py's own timeout loop, which does
// enforce its deadline correctly here (unlike wait_for_ring below).
//
// wait_for_service (kv): `<riak-admin> wait_for_service riak_kv
// <node_name>`; Extra["service"] = its own output — this port does
// NOT fail on a non-zero exit here, matching real riak.py's own
// verified source exactly: it calls run_command and stores `out`
// without ever checking `rc` for this one specific step (every other
// step in the real module DOES check rc; this one, verified by
// reading the source rather than assumed, does not).
//
// wait_for_ring (seconds > 0): polls ring state via `<riak-admin>
// ringready` (Changed... no, read-only: TRUE + "All nodes agree on
// the ring" in its own output means converged) every 10 seconds.
//
// Deviation — wait_for_ring's real timeout is effectively dead code:
// real riak.py's own `while True:` loop here can only ever exit via
// its own internal `break` on convergence; the `if time.time() >
// timeout: module.fail_json(...)` guard sits AFTER that unconditional
// `while True`, unreachable except in the narrow case where ring_check
// itself takes long enough to blow the deadline mid-call and then
// still happens to return true — in ordinary operation this means real
// riak.py's own "wait_for_ring" polls forever until the ring
// converges, never actually timing out the way its own "Number of
// seconds to wait" documentation promises. This port instead enforces
// wait_for_ring as a REAL wall-clock deadline (Fail with "Timeout
// waiting for nodes to agree on ring." once exceeded) — a deliberate,
// documented correction (an effectively-infinite poll loop is actively
// harmful to replicate faithfully in an automation context), not a
// silent behavioral drift.
//
// Extra["ring_ready"] is always set from one final `ringready` check
// (`riak-admin ringready`), matching real riak.py's own unconditional
// closing `result["ring_ready"] = ring_check(...)`.
func moduleRiak(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	command := argString(args, "command", "")
	if command != "" && command != "ping" && command != "kv_test" && command != "join" && command != "plan" && command != "commit" {
		return Result{}, errArg("riak: command must be one of ping, kv_test, join, plan, commit, got %q", command)
	}
	targetNode := argString(args, "target_node", "riak@127.0.0.1")
	httpConn := argString(args, "http_conn", "127.0.0.1:8098")
	waitForHandoffs := argInt(args, "wait_for_handoffs", 0)
	waitForRing := argInt(args, "wait_for_ring", 0)
	waitForService := argString(args, "wait_for_service", "")
	validateCerts := argBool(args, "validate_certs", true)

	adminBin, err := riakAdminBin(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	stats, err := riakFetchStats(ctx, conn, httpConn, validateCerts)
	if err != nil {
		return Result{}, err
	}
	if stats == nil {
		return Fail("riak: Timeout, could not fetch Riak stats."), nil
	}
	nodeName, _ := stats["nodename"].(string)
	var nodes []string
	if rm, ok := stats["ring_members"].([]any); ok {
		for _, v := range rm {
			if s, ok := v.(string); ok {
				nodes = append(nodes, s)
			}
		}
	}
	ringSize := stats["ring_creation_size"]

	version, err := run(ctx, conn, "riak version")
	if err != nil {
		return Result{}, err
	}

	changed := false
	extra := map[string]any{
		"node_name": nodeName,
		"nodes":     nodes,
		"ring_size": ringSize,
		"version":   version,
	}

	switch command {
	case "ping":
		res, err := runStatus(ctx, conn, "riak ping "+shellQuote(targetNode))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(strings.TrimSpace(res.Stdout)), nil
		}
		extra["ping"] = res.Stdout

	case "kv_test":
		res, err := runStatus(ctx, conn, adminBin+" test")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(strings.TrimSpace(res.Stdout)), nil
		}
		extra["kv_test"] = res.Stdout

	case "join":
		count := 0
		for _, n := range nodes {
			if n == nodeName {
				count++
			}
		}
		if count == 1 && len(nodes) > 1 {
			extra["join"] = "Node is already in cluster or staged to be in cluster."
		} else {
			res, err := runStatus(ctx, conn, adminBin+" cluster join "+shellQuote(targetNode))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail(strings.TrimSpace(res.Stdout)), nil
			}
			extra["join"] = res.Stdout
			changed = true
		}

	case "plan":
		res, err := runStatus(ctx, conn, adminBin+" cluster plan")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(strings.TrimSpace(res.Stdout)), nil
		}
		extra["plan"] = res.Stdout
		if strings.Contains(res.Stdout, "Staged Changes") {
			changed = true
		}

	case "commit":
		res, err := runStatus(ctx, conn, adminBin+" cluster commit")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(strings.TrimSpace(res.Stdout)), nil
		}
		extra["commit"] = res.Stdout
		changed = true
	}

	if waitForHandoffs > 0 {
		deadline := time.Now().Add(time.Duration(waitForHandoffs) * time.Second)
		for {
			res, err := runStatus(ctx, conn, adminBin+" transfers")
			if err != nil {
				return Result{}, err
			}
			if strings.Contains(res.Stdout, "No transfers active") {
				extra["handoffs"] = "No transfers active."
				break
			}
			if time.Now().After(deadline) {
				return Fail("riak: Timeout waiting for handoffs."), nil
			}
			time.Sleep(10 * time.Second)
		}
	}

	if waitForService == "kv" {
		res, err := runStatus(ctx, conn, adminBin+" wait_for_service riak_kv "+shellQuote(nodeName))
		if err != nil {
			return Result{}, err
		}
		extra["service"] = res.Stdout
	} else if waitForService != "" {
		return Result{}, errArg("riak: wait_for_service must be kv, got %q", waitForService)
	}

	if waitForRing > 0 {
		deadline := time.Now().Add(time.Duration(waitForRing) * time.Second)
		for {
			if ok, err := riakRingReady(ctx, conn, adminBin); err != nil {
				return Result{}, err
			} else if ok {
				break
			}
			if time.Now().After(deadline) {
				return Fail("riak: Timeout waiting for nodes to agree on ring."), nil
			}
			time.Sleep(10 * time.Second)
		}
	}

	ringReady, err := riakRingReady(ctx, conn, adminBin)
	if err != nil {
		return Result{}, err
	}
	extra["ring_ready"] = ringReady

	res := Result{Changed: changed}
	for k, v := range extra {
		res = res.WithExtra(k, v)
	}
	return res, nil
}

// riakAdminBin picks `riak-admin` if present on the target's PATH,
// else falls back to `riak admin`, matching real riak.py's own
// module.get_bin_path("riak-admin") -> [riak_bin, "admin"] fallback.
func riakAdminBin(ctx context.Context, conn remoteexec.Connection) (string, error) {
	res, err := runStatus(ctx, conn, "command -v riak-admin")
	if err != nil {
		return "", err
	}
	if res.RC == 0 {
		return "riak-admin", nil
	}
	return "riak admin", nil
}

// riakRingReady runs `<adminBin> ringready` and reports whether its
// output both exits zero and contains "TRUE All nodes agree on the
// ring", matching real riak.py's own ring_check().
func riakRingReady(ctx context.Context, conn remoteexec.Connection, adminBin string) (bool, error) {
	res, err := runStatus(ctx, conn, adminBin+" ringready")
	if err != nil {
		return false, err
	}
	return res.RC == 0 && strings.Contains(res.Stdout, "TRUE All nodes agree on the ring"), nil
}

// riakFetchStats retries `curl` against http://<httpConn>/stats every
// 5 seconds for up to 120 seconds (matching real riak.py's own
// hardcoded retry loop) and decodes a 200 response as JSON. Returns
// nil (not an error) on a 120-second timeout, matching real riak.py's
// own module.fail_json(msg="Timeout, could not fetch Riak stats.").
func riakFetchStats(ctx context.Context, conn remoteexec.Connection, httpConn string, validateCerts bool) (map[string]any, error) {
	url := "http://" + httpConn + "/stats"
	var b strings.Builder
	b.WriteString("curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}"))
	if !validateCerts {
		b.WriteString(" -k")
	}
	b.WriteString(" " + shellQuote(url))
	cmd := b.String()

	deadline := time.Now().Add(120 * time.Second)
	for {
		res, err := runStatus(ctx, conn, cmd)
		if err != nil {
			return nil, err
		}
		if res.RC == 0 {
			body, status, perr := parseCurlStatus(res.Stdout)
			if perr == nil && status == 200 {
				var stats map[string]any
				if jerr := json.Unmarshal([]byte(body), &stats); jerr != nil {
					return nil, fmt.Errorf("riak: could not parse Riak stats: %w", jerr)
				}
				return stats, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, nil
		}
		time.Sleep(5 * time.Second)
	}
}
