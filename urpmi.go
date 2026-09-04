package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUrpmi implements Ansible's `urpmi` (community.general) module:
// installs and removes packages with Mandriva/Mageia's own `urpmi`
// package manager, via its `urpmi`/`urpme` front-ends and `rpm -q` for
// presence checks.
//
// Args: name ([]string, required, aliases package/pkg); state (one of
// absent, present, installed, removed; default "present"); update_cache
// (bool, default false) — runs `urpmi.update -a -q` first; force (bool,
// default true) — `--force` on install; no_recommends (bool, default
// true) — `--no-recommends` on install; root (string, alias
// installroot) — `--root=` on every rpm/urpmi/urpme invocation.
//
// Idempotency: install checks `rpm -q --whatprovides <name>` (matching
// real urpmi's own query_package_provides(), which checks what
// provides the name rather than the literal package name itself, so a
// virtual-package/alias name is recognized as already satisfied);
// remove checks the plainer `rpm -q <name>` (query_package()).
//
// Deviation from real urpmi: real urpmi's own install_packages()
// accumulates pending package names into a single Python STRING via
// `packages += f"'{package}' "` (each name wrapped in literal
// single-quote characters, since this is plain string concatenation,
// not shell quoting) and then appends that one string as a SINGLE
// argv entry to a `module.run_command(cmd)` list invocation, which
// bypasses the shell entirely. The result is that urpmi receives one
// argv token containing literal quote characters as part of its text
// (e.g. `"'foo' 'bar' "`) rather than one argv entry per package name
// — this reads as an unintended bug (nothing in real urpmi's own
// EXAMPLES or documented behavior suggests packages should be
// delivered to the real `urpmi` binary pre-mangled like this) rather
// than documented intent, and would make a real urpmi module's own
// multi-package install request malformed. This port instead passes
// each pending package as its own separate, properly shell-quoted argv
// token, which is what the surrounding option-building code (--force,
// --quiet, --no-recommends, --root=) obviously intends to precede.
func moduleUrpmi(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	if names == nil {
		names = argStringList(args, "package")
	}
	if names == nil {
		names = argStringList(args, "pkg")
	}
	if len(names) == 0 {
		return Result{}, errArg("urpmi: missing required argument: name (or package/pkg)")
	}
	state := argString(args, "state", "present")
	switch state {
	case "absent", "present", "installed", "removed":
	default:
		return Result{}, errArg("urpmi: state must be one of absent, present, installed, removed, got %q", state)
	}
	updateCache := argBool(args, "update_cache", false)
	force := argBool(args, "force", true)
	noRecommends := argBool(args, "no_recommends", true)
	root := argString(args, "root", argString(args, "installroot", ""))

	if updateCache {
		if _, err := run(ctx, conn, "urpmi.update -a -q"); err != nil {
			return Result{}, fmt.Errorf("urpmi: could not update package db: %w", err)
		}
	}

	if state == "installed" || state == "present" {
		return urpmiInstall(ctx, conn, names, root, force, noRecommends)
	}
	return urpmiRemove(ctx, conn, names, root)
}

func urpmiRootArgs(root string) []string {
	if root == "" {
		return nil
	}
	return []string{"--root=" + root}
}

func urpmiQueryInstalled(ctx context.Context, conn remoteexec.Connection, pkg, root string) (bool, error) {
	tokens := append([]string{"rpm", "-q", pkg}, urpmiRootArgs(root)...)
	res, err := runStatus(ctx, conn, quoteAll(tokens))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

func urpmiQueryProvides(ctx context.Context, conn remoteexec.Connection, pkg, root string) (bool, error) {
	tokens := append([]string{"rpm", "-q", "--whatprovides", pkg}, urpmiRootArgs(root)...)
	res, err := runStatus(ctx, conn, quoteAll(tokens))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

func urpmiRemove(ctx context.Context, conn remoteexec.Connection, names []string, root string) (Result, error) {
	count := 0
	for _, pkg := range names {
		installed, err := urpmiQueryInstalled(ctx, conn, pkg, root)
		if err != nil {
			return Result{}, err
		}
		if !installed {
			continue
		}
		tokens := append([]string{"urpme", "--auto"}, urpmiRootArgs(root)...)
		tokens = append(tokens, pkg)
		res, err := runStatus(ctx, conn, quoteAll(tokens))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Result{}, fmt.Errorf("urpmi: failed to remove %s: %s", pkg, strings.TrimSpace(res.Stderr))
		}
		count++
	}
	if count > 0 {
		return Changed(fmt.Sprintf("removed %d package(s)", count)), nil
	}
	return Ok("package(s) already absent"), nil
}

func urpmiInstall(ctx context.Context, conn remoteexec.Connection, names []string, root string, force, noRecommends bool) (Result, error) {
	var pending []string
	for _, pkg := range names {
		provided, err := urpmiQueryProvides(ctx, conn, pkg, root)
		if err != nil {
			return Result{}, err
		}
		if !provided {
			pending = append(pending, pkg)
		}
	}
	if len(pending) == 0 {
		return Ok("package(s) already present"), nil
	}

	tokens := []string{"urpmi", "--auto"}
	if force {
		tokens = append(tokens, "--force")
	}
	tokens = append(tokens, "--quiet")
	if noRecommends {
		tokens = append(tokens, "--no-recommends")
	}
	tokens = append(tokens, urpmiRootArgs(root)...)
	tokens = append(tokens, pending...)
	res, err := runStatus(ctx, conn, quoteAll(tokens))
	if err != nil {
		return Result{}, err
	}

	for _, pkg := range names {
		provided, err := urpmiQueryProvides(ctx, conn, pkg, root)
		if err != nil {
			return Result{}, err
		}
		if !provided {
			return Result{}, fmt.Errorf("urpmi: 'urpmi %s' failed: %s", pkg, strings.TrimSpace(res.Stderr))
		}
	}
	if res.RC != 0 {
		return Result{}, fmt.Errorf("urpmi: 'urpmi %s' failed: %s", strings.Join(pending, " "), strings.TrimSpace(res.Stderr))
	}
	return Changed(strings.Join(pending, ", ") + " present(s)"), nil
}
