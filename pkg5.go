package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePkg5 implements (a subset of) Ansible's `pkg5` module: manages
// packages via Solaris 11+'s Image Packaging System (`pkg install`/
// `pkg uninstall`).
//
// Args: name (string or []string, required) — an FRMI, or list of
// FRMIs, of the package(s) to install/remove/update; state
// (present|installed|latest|absent|removed|uninstalled, default
// "present"); accept_licenses (bool, default false, aliases accept/
// accept_licences) — `--accept`; be_name (string, optional) —
// `--be-name=<name>`, creates a new boot environment; refresh (bool,
// default true) — omitting it adds `--no-refresh` (skip refreshing
// publishers before the operation); verbose (bool, default false) —
// omitting it adds `-q` (real pkg5 is quiet by default and verbose
// disables that).
//
// Simplifications vs real pkg5: real pkg5 additionally re-joins FRMI
// fragments that Ansible's own comma-splitting of a plain string
// argument breaks apart (e.g. "editor/vim@1.2,5.11" splitting into two
// list items at the comma) — not applicable here, since this port's
// `name` arrives as an already-assembled list, never a comma-string
// Ansible itself re-splits. Idempotency for present/absent is checked
// via `pkg list -- <name>` (exit 0 iff installed); state=latest always
// runs `pkg install` and reports changed (this batch's house "can't
// cheaply tell already-latest apart" convention — real pkg5 itself
// does check via a separate `pkg list -u` probe per package, but this
// port does not replicate that second probe). check_mode/dry-run
// (`pkg install -n`) is not modeled, since this port has no check_mode
// concept (see zfs_delegate_admin.go's own doc comment for the same
// note).
func modulePkg5(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("pkg5: %v", err)
	}
	state := argString(args, "state", "present")

	flags := ""
	accept := argBool(args, "accept_licenses", argBool(args, "accept", argBool(args, "accept_licences", false)))
	if accept {
		flags += " --accept"
	}
	if beName := argString(args, "be_name", ""); beName != "" {
		flags += " --be-name=" + beName
	}
	if !argBool(args, "refresh", true) {
		flags += " --no-refresh"
	}
	if !argBool(args, "verbose", false) {
		flags += " -q"
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			res, err := conn.Exec(ctx, "pkg list -- "+shellQuote(name)+" >/dev/null 2>&1", nil)
			if err != nil {
				return false, fmt.Errorf("checking pkg5 package %s: %w", name, err)
			}
			return res.RC == 0, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pkg install"+flags+" -- "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pkg uninstall"+flags+" -- "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, "pkg install"+flags+" -- "+quoteAll(names))
			return err
		},
	)
}
