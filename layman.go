package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLayman implements Ansible's `layman` (community.general)
// module: installs, syncs, or uninstalls a Gentoo Portage overlay via
// the `layman` CLI. Real Gentoo deprecated `layman` itself in mid-2023
// and real community.general.layman is itself deprecated for removal;
// this port keeps the same CLI-composition approach as portage.go's own
// module for the same reason.
//
// Architectural note: real layman's own implementation imports the
// `layman` Python API directly and downloads an alternative overlay
// list itself via `open_url` before invoking that API — none of which
// this port's Connection can reach. This port instead composes the
// `layman` command-line tool itself (present on any host with layman
// installed, per real layman's own REQUIREMENTS), matching this batch's
// own assignment brief.
//
// Args: name (string, required) — the overlay ID, or "ALL" (only valid
// with state=updated) to sync every installed overlay; state
// (present|absent|updated, default "present"); list_url (string, alias
// url) — an alternative overlay-definitions list URL, passed to
// `layman -o <url>` before `-a`; validate_certs (bool, default true) —
// accepted for argspec compatibility, not honored (this port never
// makes an HTTP request itself here; `layman`'s own TLS validation
// behavior for `-o` is controlled by ITS OWN configuration, not a
// per-invocation flag this port can set).
//
// State semantics: present is idempotent against `layman -l`'s own
// installed-overlay listing; absent likewise; updated for a specific
// name installs it first (with list_url, if given) if not already
// installed — matching real layman's own doc ("sync the overlay ... or
// install if not installed yet") — then always runs `layman -s <name>`
// and always reports Changed (real layman's own sync has no
// idempotency check to report against, matching this port's general
// unconditional-action-verb convention, see monit.go's own doc
// comment); name="ALL" always runs `layman -s ALL` and always reports
// Changed.
func moduleLayman(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	listURL := argString(args, "list_url", argString(args, "url", ""))

	if name == "ALL" && state != "updated" {
		return Result{}, errArg("layman: name=ALL is only valid when state=updated")
	}

	switch state {
	case "updated":
		if name == "ALL" {
			if _, err := run(ctx, conn, "layman -s ALL"); err != nil {
				return Result{}, err
			}
			return Changed("synced all overlays"), nil
		}
		installed, err := laymanInstalled(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if !sliceHasString(installed, name) {
			if err := laymanAdd(ctx, conn, name, listURL); err != nil {
				return Result{}, err
			}
		}
		if _, err := run(ctx, conn, "layman -s "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " synced"), nil

	case "present":
		installed, err := laymanInstalled(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if sliceHasString(installed, name) {
			return Ok(name + " already installed"), nil
		}
		if err := laymanAdd(ctx, conn, name, listURL); err != nil {
			return Result{}, err
		}
		return Changed(name + " installed"), nil

	case "absent":
		installed, err := laymanInstalled(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if !sliceHasString(installed, name) {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "layman -d "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " uninstalled"), nil

	default:
		return Result{}, errArg("layman: state must be present, absent, or updated, got %q", state)
	}
}

func laymanAdd(ctx context.Context, conn remoteexec.Connection, name, listURL string) error {
	cmd := "layman"
	if listURL != "" {
		cmd += " -o " + shellQuote(listURL)
	}
	cmd += " -a " + shellQuote(name)
	_, err := run(ctx, conn, cmd)
	return err
}

// laymanInstalled returns the currently-installed overlay names, from
// `layman -l`'s own one-per-line listing (each line's first
// whitespace-delimited field is the overlay ID).
func laymanInstalled(ctx context.Context, conn remoteexec.Connection) ([]string, error) {
	res, err := runStatus(ctx, conn, "layman -l 2>/dev/null")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range splitLines(res.Stdout) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		names = append(names, fields[0])
	}
	return names, nil
}
