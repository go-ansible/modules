package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHomebrewTap implements (a subset of) Ansible's `homebrew_tap`
// module: taps or untaps a Homebrew third-party repository, optionally
// also setting whether Homebrew trusts it.
//
// Args: name (string or []string, required) — the tap(s), e.g.
// "homebrew/dupes"; url (string, optional) — an alternate git URL to
// tap from, only valid with a single name; trust (bool, optional,
// three-state: unset leaves the tap's current trust state untouched) —
// true runs `brew trust <tap>`, false runs `brew untrust <tap>`;
// combining trust=true with state=absent is rejected, matching real
// homebrew_tap's own documented error; state (present|absent, default
// "present").
//
// Simplifications vs real homebrew_tap: no `path` support. `brew
// trust`/`brew untrust` are provided by a third-party tap, not core
// Homebrew (real homebrew_tap's own doc: "This requires a Homebrew
// version providing the `brew trust' command") — this port does not
// special-case that command's absence; if it's missing, `run` surfaces
// the shell's own "command not found" failure. Idempotency for trust is
// checked via `brew trust --json v1`, matching on the tap name as a
// plain substring of the JSON output rather than fully parsing it —
// this batch's house best-effort text-check convention (see debconf.go)
// — so a tap name that is also a substring of another trusted tap's
// name could false-positive; real Homebrew tap names (`user/repo`) make
// that collision unlikely in practice.
func moduleHomebrewTap(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("homebrew_tap: %v", err)
	}
	url := argString(args, "url", "")
	state := argString(args, "state", "present")
	if url != "" && len(names) != 1 {
		return Result{}, errArg("homebrew_tap: url requires exactly one name")
	}
	_, trustSet := args["trust"]
	trust := argBool(args, "trust", false)
	if trustSet && trust && state == "absent" {
		return Result{}, errArg("homebrew_tap: combining trust=true with state=absent is an error")
	}

	res, err := pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			r, err := conn.Exec(ctx, "brew tap 2>/dev/null | grep -qxF "+shellQuote(name), nil)
			if err != nil {
				return false, fmt.Errorf("checking tap %s: %w", name, err)
			}
			return r.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			for _, name := range names {
				cmd := "brew tap " + shellQuote(name)
				if url != "" {
					cmd += " " + shellQuote(url)
				}
				if _, err := run(ctx, conn, cmd); err != nil {
					return err
				}
			}
			return nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "brew untap "+quoteAll(names))
			return err
		},
		nil,
	)
	if err != nil {
		return Result{}, err
	}
	if res.Failed || !trustSet || state == "absent" {
		return res, nil
	}

	trustChanged := false
	for _, name := range names {
		already, err := homebrewTapTrusted(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if already == trust {
			continue
		}
		verb := "untrust"
		if trust {
			verb = "trust"
		}
		if _, err := run(ctx, conn, "brew "+verb+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		trustChanged = true
	}
	if trustChanged {
		return Changed("updated trust"), nil
	}
	return res, nil
}

// homebrewTapTrusted makes a best-effort check for whether name already
// appears trusted, per moduleHomebrewTap's doc comment on its
// substring-match limitation.
func homebrewTapTrusted(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "brew trust --json v1 2>/dev/null | grep -qF "+shellQuote(name))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
