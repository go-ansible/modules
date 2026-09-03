package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOpkg implements (a subset of) Ansible's `opkg` module: manages
// ipk packages via OpenWrt/Yocto's `opkg`.
//
// Args: name (string or []string, aliases "pkg" via the args map
// directly — see below) — package name(s), or "NAME=VERSION" (Yocto
// only, opkg>=0.3.2; not supported by plain OpenWrt opkg); state
// (present|installed|absent|removed, default "present"); update_cache
// (bool, default false) — runs `opkg update` first; force (string,
// optional) — one of depends|maintainer|reinstall|overwrite|downgrade|
// space|postinstall|remove|checksum|removal-of-dependent-packages,
// passed through verbatim as `--force-<value>`; executable (string,
// optional) — path to `opkg`, default "opkg".
//
// Simplifications vs real opkg: no `latest` state — real opkg has none
// either, so this matches rather than deviates (pkgManagerLoop's
// latest hook is passed nil, so state=latest fails with a clear error
// instead of silently behaving like present). This port does not
// validate `force` against opkg's own choice list. Idempotency for
// present/absent is checked via `opkg list-installed` (matching a line
// starting with the bare package name, i.e. ignoring any "=VERSION"
// suffix) — so re-running with a different pinned version on an
// already-installed package is not detected as a change, an
// acknowledged gap since opkg has no simple "is this exact version
// installed" one-liner.
func moduleOpkg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	nameArgs := args
	if _, ok := args["name"]; !ok {
		if v, ok := args["pkg"]; ok {
			nameArgs = map[string]any{"name": v}
		}
	}
	names, err := resolveNames(nameArgs)
	if err != nil {
		return Result{}, errArg("opkg: %v", err)
	}
	state := argString(args, "state", "present")
	exe := argString(args, "executable", "opkg")
	force := argString(args, "force", "")

	forceFlag := ""
	if force != "" {
		forceFlag = " --force-" + force
	}

	if argBool(args, "update_cache", false) {
		if _, err := run(ctx, conn, exe+" update"); err != nil {
			return Result{}, err
		}
	}

	return pkgManagerLoop(ctx, conn, names, state,
		func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
			base, _, _ := strings.Cut(name, "=")
			out, err := run(ctx, conn, exe+" list-installed")
			if err != nil {
				return false, err
			}
			for _, line := range strings.Split(out, "\n") {
				if line == base || strings.HasPrefix(line, base+" ") {
					return true, nil
				}
			}
			return false, nil
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			_, err := run(ctx, conn, exe+" install"+forceFlag+" "+quoteAll(names))
			return err
		},
		func(ctx context.Context, conn remoteexec.Connection, names []string) error {
			var base []string
			for _, n := range names {
				b, _, _ := strings.Cut(n, "=")
				base = append(base, b)
			}
			_, err := run(ctx, conn, exe+" remove"+forceFlag+" "+quoteAll(base))
			return err
		},
		nil,
	)
}
