package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFlatpak implements (a subset of) Ansible's `flatpak` module:
// installs or removes flatpaks.
//
// Args: name (string or []string) — reverse-DNS application ID(s), or
// (for state=present only) a `.flatpakref' URL; from_url (string,
// optional) — install from this flatpakref URL, with name giving the
// application ID to check for; remote (string, default "flathub");
// method (system|user, default "system"); state (present|absent|
// latest, default "present").
//
// Simplifications vs real flatpak: no `no_dependencies` or custom
// `executable` support. Idempotency for present/absent is checked via
// `flatpak info <name>` (exit 0 iff installed); state=latest always
// runs `flatpak update -y <name>` and reports changed, matching this
// batch's house "can't cheaply tell already-latest apart" convention
// (see apt.go).
func moduleFlatpak(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("flatpak: %v", err)
	}
	fromURL := argString(args, "from_url", "")
	remote := argString(args, "remote", "flathub")
	method := argString(args, "method", "system")
	if method != "system" && method != "user" {
		return Result{}, errArg("flatpak: method must be system or user, got %q", method)
	}
	state := argString(args, "state", "present")
	methodFlag := "--" + method

	if fromURL != "" {
		if len(names) != 1 {
			return Result{}, errArg("flatpak: from_url requires exactly one name (the application ID to check for)")
		}
		if state != "present" {
			return Result{}, errArg("flatpak: from_url is only valid with state=present")
		}
		installed, err := flatpakInstalled(ctx, conn, methodFlag, names[0])
		if err != nil {
			return Result{}, err
		}
		if installed {
			return Ok(names[0] + " already installed"), nil
		}
		if _, err := run(ctx, conn, "flatpak install -y "+methodFlag+" "+shellQuote(fromURL)); err != nil {
			return Result{}, err
		}
		return Changed(names[0] + " installed"), nil
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			return flatpakInstalled(ctx, conn, methodFlag, name)
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			for _, name := range names {
				if _, err := run(ctx, conn, "flatpak install -y "+methodFlag+" "+shellQuote(remote)+" "+shellQuote(name)); err != nil {
					return err
				}
			}
			return nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "flatpak uninstall -y "+methodFlag+" "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "flatpak update -y "+methodFlag+" "+quoteAll(names))
			return err
		},
	)
}

func flatpakInstalled(ctx context.Context, conn remoteexec.Connection, methodFlag, name string) (bool, error) {
	res, err := conn.Exec(ctx, "flatpak info "+methodFlag+" "+shellQuote(name)+" >/dev/null 2>&1", nil)
	if err != nil {
		return false, fmt.Errorf("checking flatpak %s: %w", name, err)
	}
	return res.RC == 0, nil
}
