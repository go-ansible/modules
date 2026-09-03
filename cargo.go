package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCargo implements (a subset of) Ansible's `cargo` module: manages
// Rust binaries via `cargo install`/`cargo uninstall`.
//
// Args: name (string or []string) — crate name(s) from the configured
// registry (crates.io by default); directory (string, optional) —
// installs FROM a local source directory instead (`cargo install
// --path`), only meaningful for present/latest; executable (string,
// default "cargo"); features ([]string, optional) — joined with commas
// into `--features`; locked (bool, default false); path (string,
// optional) — passed as `--root` (cargo appends "/bin" itself); state
// (present|absent|latest, default "present"); version (string,
// optional) — only valid with a single, non-directory name (matching
// real cargo's own restriction that `--version` applies to one crate at
// a time).
//
// Simplifications vs real cargo: cargo has no query subcommand as clean
// as `dpkg -s`; idempotency for present/absent is checked by grepping
// `cargo install --list` for a line starting with "<name> v" — this
// only detects presence, not whether the installed version matches a
// requested `version` (re-running present with a different pinned
// version on an already-present crate is a no-op here, the same
// limitation npm.go documents for its own `version`). state=latest
// always passes `--force` and reports changed (this batch's house
// "can't cheaply tell already-latest apart" convention, see apt.go).
// `directory` mode (installing from a local path) has no query
// mechanism at all — like bundler.go, it always runs and reports
// changed, and it does not support state=absent (there is no registry
// name to look up in order to uninstall).
func moduleCargo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	exe := argString(args, "executable", "cargo")
	directory := argString(args, "directory", "")
	path := argString(args, "path", "")
	locked := argBool(args, "locked", false)
	features := argStringList(args, "features")
	version := argString(args, "version", "")
	names := argStringList(args, "name")

	if directory == "" && len(names) == 0 {
		return Result{}, errArg("cargo: missing required argument: name (or directory)")
	}

	flags := ""
	if locked {
		flags += " --locked"
	}
	if len(features) > 0 {
		flags += " --features " + shellQuote(strings.Join(features, ","))
	}
	if path != "" {
		flags += " --root " + shellQuote(path)
	}

	if directory != "" {
		if state == "absent" {
			return Result{}, errArg("cargo: directory is only used when installing packages, not with state=absent")
		}
		cmd := exe + " install --path " + shellQuote(directory) + flags
		if state == "latest" {
			cmd += " --force"
		}
		if version != "" {
			cmd += " --version " + shellQuote(version)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("cargo install --path " + directory), nil
	}

	if version != "" && len(names) != 1 {
		return Result{}, errArg("cargo: version can only be used with a single name")
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			return cargoInstalled(ctx, conn, exe, name)
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			if version != "" {
				_, err := run(ctx, conn, exe+" install"+flags+" --version "+shellQuote(version)+" "+quoteAll(names))
				return err
			}
			_, err := run(ctx, conn, exe+" install"+flags+" "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, exe+" uninstall "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			cmd := exe + " install --force" + flags
			if version != "" {
				cmd += " --version " + shellQuote(version)
			}
			cmd += " " + quoteAll(names)
			_, err := run(ctx, conn, cmd)
			return err
		},
	)
}

func cargoInstalled(ctx context.Context, conn remoteexec.Connection, exe, name string) (bool, error) {
	out, err := run(ctx, conn, exe+" install --list")
	if err != nil {
		return false, err
	}
	prefix := name + " v"
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true, nil
		}
	}
	return false, nil
}
