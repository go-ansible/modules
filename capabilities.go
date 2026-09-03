package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCapabilities implements (a subset of) Ansible's `capabilities`
// module: manages one Linux file capability via `setcap`/`getcap`.
//
// Args: path (string, required; aliased from `key`); capability
// (string, required; aliased from `cap`) — e.g. "cap_net_raw+ep";
// state (present|absent, default "present").
//
// Idempotency for state=present is a plain substring check of `getcap
// <path>`'s output against the literal capability string — matching
// real capabilities' own documented NOTE that it "does not attempt to
// determine the final operator and flags to compare", so a
// capability given as `cap_foo=ep` that the kernel reports back as
// `cap_foo+ep` will look different byte-for-byte and be treated as not
// yet present (real capabilities has the identical limitation).
//
// state=absent removes only the named capability, not the whole set:
// this port parses getcap's comma-separated capability list, drops any
// entry whose capability name (the text before its +/-/= operator)
// matches, and re-applies the remaining set via `setcap` (or `setcap
// -r` if nothing remains) — getcap's exact output format (a `path =
// caps` vs `path caps` separator) has varied across libcap versions, so
// this port strips path as a prefix rather than parsing a fixed
// delimiter, another best-effort heuristic in the same spirit as
// apt_key.go's own listing-output grep.
//
// Simplification vs real capabilities: no diff_mode support beyond what
// Result already offers.
func moduleCapabilities(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := capabilitiesRequirePath(args)
	if err != nil {
		return Result{}, err
	}
	capability, err := capabilitiesRequireCapability(args)
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	capName := capabilitiesBaseName(capability)

	current, err := capabilitiesGet(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		for _, c := range current {
			if c == capability {
				return Ok(path + " already has " + capability), nil
			}
		}
		cmd := "setcap " + shellQuote(capability) + " " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(path + " granted " + capability), nil

	case "absent":
		var remaining []string
		found := false
		for _, c := range current {
			if capabilitiesBaseName(c) == capName {
				found = true
				continue
			}
			remaining = append(remaining, c)
		}
		if !found {
			return Ok(path + " already lacks " + capability), nil
		}
		var cmd string
		if len(remaining) == 0 {
			cmd = "setcap -r " + shellQuote(path)
		} else {
			cmd = "setcap " + shellQuote(strings.Join(remaining, ",")) + " " + shellQuote(path)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(path + " removed " + capability), nil

	default:
		return Result{}, errArg("capabilities: state must be present or absent, got %q", state)
	}
}

func capabilitiesRequirePath(args map[string]any) (string, error) {
	if s, ok := args["path"].(string); ok && s != "" {
		return s, nil
	}
	if s, ok := args["key"].(string); ok && s != "" {
		return s, nil
	}
	return "", errArg("capabilities: path (or its alias key) is required")
}

func capabilitiesRequireCapability(args map[string]any) (string, error) {
	if s, ok := args["capability"].(string); ok && s != "" {
		return s, nil
	}
	if s, ok := args["cap"].(string); ok && s != "" {
		return s, nil
	}
	return "", errArg("capabilities: capability (or its alias cap) is required")
}

// capabilitiesBaseName strips a capability string's trailing
// operator+flags (+ep, -ep, =ep, ...), returning just its name, e.g.
// "cap_net_raw+ep" -> "cap_net_raw".
func capabilitiesBaseName(cap string) string {
	if i := strings.IndexAny(cap, "+-="); i >= 0 {
		return cap[:i]
	}
	return cap
}

// capabilitiesGet parses `getcap <path>`'s output into its
// comma-separated list of "cap_name+flags" tokens, tolerating both the
// "path = caps" and "path caps" separator forms seen across libcap
// versions, and an absent/empty result (no capabilities set).
func capabilitiesGet(ctx context.Context, conn remoteexec.Connection, path string) ([]string, error) {
	res, err := runStatus(ctx, conn, "getcap "+shellQuote(path)+" 2>/dev/null")
	if err != nil {
		return nil, err
	}
	out := strings.TrimSpace(res.Stdout)
	if res.RC != 0 || out == "" {
		return nil, nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(out, path))
	rest = strings.TrimPrefix(rest, "=")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil, nil
	}
	var caps []string
	for _, c := range strings.Split(rest, ",") {
		c = strings.TrimSpace(c)
		if c != "" {
			caps = append(caps, c)
		}
	}
	return caps, nil
}
