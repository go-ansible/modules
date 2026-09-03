package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePear implements (a subset of) Ansible's `pear` module: manages
// PHP packages via the `pear`/`pecl` package managers.
//
// Args: name (string, aliases "pkg" via the args map directly — see
// below) — one package, or several comma-separated (e.g.
// "Net_URL2,pecl/json_post"), matching real pear's own single-string,
// comma-joined convention (NOT a YAML list, unlike most of this batch);
// state (present|installed|latest|absent|removed, default "present");
// executable (string, optional) — path to the `pear` binary, default
// "pear" (does NOT override the `pecl` binary used for "pecl/"-prefixed
// package names — real pear.py may expose a way to do that, but it
// isn't in the fetched doc text, so this is a documented gap rather
// than a verified simplification).
//
// Simplifications vs real pear: `prompts` (regex-driven interactive
// answers for PECL install-time questions) is accepted syntactically but
// is a no-op here — this port has no PTY/expect-style interaction with
// the target's stdin, so an install that would need an interactive
// answer just runs non-interactively (may fail, or take the tool's own
// default answer, depending on the target's pecl build). A
// "pecl/"-prefixed name is dispatched to the `pecl` binary and installed
// under its unprefixed name, matching real pear.py's documented
// pecl/json_post example. Idempotency for present/absent is checked via
// `pear list`/`pecl list` (matching a line starting with the bare
// package name) — this only sees the default channel, so a package
// installed under a non-default channel may be reported as
// not-installed.
func modulePear(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	nameArgs := args
	if _, ok := args["name"]; !ok {
		if v, ok := args["pkg"]; ok {
			nameArgs = map[string]any{"name": v}
		}
	}
	raw, err := requireString(nameArgs, "name")
	if err != nil {
		return Result{}, errArg("pear: %v", err)
	}
	var names []string
	for _, n := range strings.Split(raw, ",") {
		n = strings.TrimSpace(n)
		if n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return Result{}, errArg("pear: missing required argument: name")
	}
	state := argString(args, "state", "present")
	pearBin := argString(args, "executable", "pear")

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			bin, pkg := pearBinaryFor(pearBin, name)
			out, err := run(ctx, conn, bin+" list")
			if err != nil {
				return false, err
			}
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, pkg+" ") {
					return true, nil
				}
			}
			return false, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			for _, n := range names {
				bin, pkg := pearBinaryFor(pearBin, n)
				if _, err := run(ctx, conn, bin+" install "+shellQuote(pkg)); err != nil {
					return err
				}
			}
			return nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			for _, n := range names {
				bin, pkg := pearBinaryFor(pearBin, n)
				if _, err := run(ctx, conn, bin+" uninstall "+shellQuote(pkg)); err != nil {
					return err
				}
			}
			return nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			for _, n := range names {
				bin, pkg := pearBinaryFor(pearBin, n)
				if _, err := run(ctx, conn, bin+" upgrade "+shellQuote(pkg)); err != nil {
					return err
				}
			}
			return nil
		},
	)
}

// pearBinaryFor returns the binary to invoke and the unprefixed package
// name for a pear/pecl package spec: "pecl/foo" dispatches to the pecl
// binary (not overridable via the module's own `executable` arg, which
// only names the pear binary), everything else uses pearBin.
func pearBinaryFor(pearBin, name string) (bin, pkg string) {
	if strings.HasPrefix(name, "pecl/") {
		return "pecl", strings.TrimPrefix(name, "pecl/")
	}
	return pearBin, name
}
