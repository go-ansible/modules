package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOhai implements Ansible's `ohai` module: runs Chef's real Ohai
// discovery program on the target and returns its JSON inventory data,
// read from real ohai.py's own source (plugins/modules/ohai.py) — a
// thin wrapper: `/usr/bin/env ohai`, parsed and returned.
//
// Args: none (real ohai has an empty argument_spec).
//
// ⚠ Ohai's parsed JSON keys land at the TOP LEVEL of the module result,
// NOT nested under `ansible_facts` — a real, verified quirk, and a
// deliberate difference from moduleFacterFacts's own behavior right
// next to it in this port. Verified directly from real ohai.py's own
// main(): `module.exit_json(**json.loads(out))` spreads Ohai's own
// top-level JSON object keys directly into the module's exit_json
// kwargs — unlike facter_facts, which wraps its JSON under
// `ansible_facts={"facter": ...}`. Because Ohai's own top-level keys
// become the result's own top-level fields rather than
// `ansible_facts.*`, real ohai's own data does NOT get automatically
// merged into host facts/`hostvars` the way facter_facts's or setup's
// own data does — a caller wanting Ohai's data available as facts must
// `register:` the task and reference `result.<key>` explicitly, or run
// it through `set_fact`. This port replicates that exactly: every
// top-level key from Ohai's own JSON output is set via Result.Extra
// (WithExtra), and Result.Facts is left nil — nothing here is merged
// into ansible_facts.
//
// Binary resolution: real ohai hard-codes `/usr/bin/env ohai` (relying
// on `ohai` being on PATH; it does not use get_bin_path or any fallback
// directory the way facter_facts does) — this port does the same, via a
// `command -v ohai` PATH check, Fail()ing cleanly (Result{Failed:true},
// not a Go error) if it's missing, matching this port's own "external
// tool not installed" convention (e.g. ipaRequireBinary,
// pip_package_info.go).
//
// Never Changed — real ohai's own check_mode support is "none" (it
// doesn't special-case check mode at all, since it's read-only anyway);
// this port always reports unchanged for the same reason.
//
// Deviation vs real ohai: this port does not set LANGUAGE=C/LC_ALL=C
// the way real ohai does (`module.run_command_environ_update`) — see
// moduleFacterFacts's own doc comment for why and how this port
// approximates it with a plain `env` prefix instead.
func moduleOhai(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := run(ctx, conn, "command -v ohai"); err != nil {
		return Fail("ohai: the ohai binary (from Chef, https://docs.chef.io/ohai.html) is required on the " +
			"target and was not found in PATH"), nil
	}

	res, err := runStatus(ctx, conn, "env LANGUAGE=C LC_ALL=C ohai")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail("ohai: " + msg), nil
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		return Fail("ohai: parsing ohai's JSON output: " + err.Error()), nil
	}

	result := Ok("gathered ohai inventory data")
	for k, v := range parsed {
		result = result.WithExtra(k, v)
	}
	return result, nil
}
