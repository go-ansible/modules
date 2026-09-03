package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSnap implements (a subset of) Ansible's `snap` module: manages
// snap packages via `snap install`/`remove`/`enable`/`disable`.
//
// Args: name (string or []string, required); state (present|absent|
// enabled|disabled, default "present"); channel (string, optional —
// only valid for a single-package task, matching real snap's own
// constraint); classic (bool, default false) — passes `--classic'.
//
// Simplifications vs real snap: no `options` (snap set), `revision`,
// or `dangerous`/`devmode' support, and the special "system" pseudo-
// name (snapd's own configuration namespace) is not handled specially.
// Idempotency for present/absent is checked via `snap list <name>`
// (exit 0 iff installed); enabled/disabled toggle `snap enable`/`snap
// disable`, checked via the "notes" column of `snap list <name>`
// containing "disabled" (a plain substring check, not the more
// structured `snap info --unicode=never` output — matching this
// project's broader convention of a best-effort textual check where
// parsing the real structured output would cost more than it is worth,
// see debconf.go).
func moduleSnap(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("snap: %v", err)
	}
	state := argString(args, "state", "present")
	channel := argString(args, "channel", "")
	classic := argBool(args, "classic", false)
	if channel != "" && len(names) != 1 {
		return Result{}, errArg("snap: channel can only be specified for a single snap")
	}

	flags := ""
	if classic {
		flags += " --classic"
	}
	if channel != "" {
		flags += " --channel=" + shellQuote(channel)
	}

	switch state {
	case "enabled":
		return snapToggle(ctx, conn, names, "enable", false)
	case "disabled":
		return snapToggle(ctx, conn, names, "disable", true)
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			res, err := conn.Exec(ctx, "snap list "+shellQuote(name)+" >/dev/null 2>&1", nil)
			if err != nil {
				return false, fmt.Errorf("checking snap %s: %w", name, err)
			}
			return res.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "snap install"+flags+" "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "snap remove "+quoteAll(names))
			return err
		},
		nil,
	)
}

// snapToggle implements the enabled/disabled states: wantDisabled is
// true for state=disabled.
func snapToggle(ctx context.Context, conn remoteexec.Connection, names []string, verb string, wantDisabled bool) (Result, error) {
	var toToggle []string
	for _, name := range names {
		res, err := runStatus(ctx, conn, "snap list "+shellQuote(name)+" 2>/dev/null | awk 'NR==2{print $NF}'")
		if err != nil {
			return Result{}, err
		}
		isDisabled := res.RC == 0 && contains(res.Stdout, "disabled")
		if isDisabled != wantDisabled {
			toToggle = append(toToggle, name)
		}
	}
	if len(toToggle) == 0 {
		return Ok("unchanged"), nil
	}
	if _, err := run(ctx, conn, "snap "+verb+" "+quoteAll(toToggle)); err != nil {
		return Result{}, err
	}
	return Changed(verb + "d"), nil
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
