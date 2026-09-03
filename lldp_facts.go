package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLLDPFacts implements Ansible's `lldp_facts` (community.general,
// renamed from `lldp` in community.general 13.0.0) module: gathers
// LLDP neighbor facts by parsing `lldpctl -f keyvalue` output into
// Extra["lldp"] — read from real lldp_facts.py's own gather_lldp
// function (this batch's hard rule: the nested-path-building and
// multivalue/continuation-line rules below are only visible there, not
// EXAMPLES/RETURN VALUES).
//
// Args: multivalues (bool, default false) — when an attribute appears
// more than once for the same path (e.g. multiple VLANs), represent
// every occurrence as a list under that path instead of keeping only
// the last one.
//
// `lldpctl -f keyvalue` prints one "lldp.<ifname>.<...path...>.<leaf>=
// <value>" line per attribute (e.g.
// "lldp.eth0.chassis.name=switch1.example.com"), plus occasional
// CONTINUATION lines (not starting with "lldp") that extend the
// PREVIOUS line's own value across multiple lines — real gather_lldp's
// own parsing loop tracks this as "final" (the most recent leaf key)
// and appends "\n<line>" to whatever is currently stored there,
// reproduced identically here. Each dotted path segment becomes a
// nested map level; if an earlier line already stored a plain leaf
// value exactly where a later line needs to nest further (a path
// collision — e.g. "lldp.eth0.foo=x" followed by
// "lldp.eth0.foo.bar=y"), that leaf is wrapped as {"value": <leaf>}
// and nesting continues from there, matching real gather_lldp's own
// `if not isinstance(current_dict[path_component], dict):
// current_dict[path_component] = {"value": ...}` — an edge case this
// port reproduces for fidelity even though it is rare in practice.
// When multivalues is true AND the target leaf key already holds a
// dict (from exactly that same collision case), real gather_lldp
// redirects into that dict's own "value" key instead, which this port
// also reproduces.
//
// Matching real gather_lldp's own module.get_bin_path("lldpctl")
// (called without required=True, and never checked for None before
// use — an apparent upstream oversight that would crash the Python
// interpreter outright if lldpctl is missing), this port does NOT
// gate on `command -v lldpctl` first: it just runs `lldpctl -f
// keyvalue` and, since a missing binary makes the shell itself report
// "command not found" with empty output, falls into the same "empty
// output -> fail cleanly" path described below — a strictly BETTER
// outcome than real lldp_facts' own crash, not a narrowing.
//
// Real gather_lldp does not check lldpctl's own exit code at all —
// only whether its stdout is non-empty. If EMPTY, gather_lldp returns
// None, and real lldp_facts' own main() then hits `lldp_output["lldp"]`
// on a None, raising TypeError, caught and turned into
// module.fail_json("lldpctl command failed. is lldpd running?") — this
// port reproduces exactly that failure message for empty output. If
// the output is non-empty but happens to contain no "lldp"-prefixed
// line and thus never populates a top-level "lldp" key (only reachable
// with a badly-behaved or unexpected lldpctl build), real lldp_facts'
// own code path there would actually raise an uncaught KeyError
// instead (looking up "lldp" on a dict that doesn't have it) — NOT
// caught by its own `except TypeError`, an apparent second upstream
// oversight; this port does not reproduce that crash and instead
// returns the same clean "lldpctl command failed" failure, which
// this package's own general stance favors over replicating a literal
// interpreter crash where a clean, behavior-equivalent Fail is
// available (see this package's other modules' own doc comments for
// the same stance elsewhere).
func moduleLLDPFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	multivalues := argBool(args, "multivalues", false)

	res, err := runStatus(ctx, conn, "lldpctl -f keyvalue")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(res.Stdout) == "" {
		return Fail("lldp_facts: lldpctl command failed. is lldpd running?"), nil
	}

	parsed := lldpParseKeyValue(res.Stdout, multivalues)
	lldp, ok := parsed["lldp"].(map[string]any)
	if !ok {
		return Fail("lldp_facts: lldpctl command failed. is lldpd running?"), nil
	}

	return Ok("").WithExtra("lldp", lldp), nil
}

// lldpParseKeyValue mirrors real gather_lldp's own parsing loop over
// `lldpctl -f keyvalue` output — see moduleLLDPFacts' own doc comment
// for the path-nesting, continuation-line, and multivalue rules this
// reproduces.
func lldpParseKeyValue(out string, multivalues bool) map[string]any {
	outputDict := map[string]any{}
	var currentDict map[string]any
	var final string

	for _, entry := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(entry, "lldp") {
			trimmed := strings.TrimSpace(entry)
			keyPath, value, ok := strings.Cut(trimmed, "=")
			if !ok {
				continue
			}
			parts := strings.Split(keyPath, ".")
			pathComponents := parts[:len(parts)-1]
			final = parts[len(parts)-1]

			cur := outputDict
			for _, pc := range pathComponents {
				child, ok := cur[pc].(map[string]any)
				if !ok {
					if existing, has := cur[pc]; has {
						child = map[string]any{"value": existing}
					} else {
						child = map[string]any{}
					}
					cur[pc] = child
				}
				cur = child
			}
			currentDict = cur

			if ev, ok := currentDict[final].(map[string]any); ok && multivalues {
				currentDict = ev
				final = "value"
			}

			lldpSetLeaf(currentDict, final, value, multivalues)
			continue
		}

		// Continuation line: extend the previous leaf's own value.
		if currentDict == nil {
			continue
		}
		switch v := currentDict[final].(type) {
		case string:
			currentDict[final] = v + "\n" + entry
		case []string:
			if len(v) > 0 {
				v[len(v)-1] = v[len(v)-1] + "\n" + entry
				currentDict[final] = v
			}
		}
	}
	return outputDict
}

// lldpSetLeaf mirrors real gather_lldp's own leaf-assignment rules: set
// outright if unset or multivalues is off; otherwise promote the
// existing value into (or append to) a list.
func lldpSetLeaf(dict map[string]any, key, value string, multivalues bool) {
	existing, ok := dict[key]
	if !ok || !multivalues {
		dict[key] = value
		return
	}
	switch v := existing.(type) {
	case string:
		dict[key] = []string{v, value}
	case []string:
		dict[key] = append(v, value)
	default:
		dict[key] = value
	}
}
