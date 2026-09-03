package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGolangPackage implements (a subset of) Ansible's
// `golang_package` module: installs/removes Go binaries via `go
// install`.
//
// Args: name (string or []string, required) — a full Go package import
// path, optionally with an inline `@version` suffix (e.g.
// "golang.org/x/tools/cmd/stringer@v0.29.0"); state
// (present|absent|latest, default "present"); version (string, optional)
// — applies to a single, non-inline-versioned name only, matching real
// golang_package's own restriction; executable (string, optional) —
// path to the `go` binary, default "go".
//
// Simplifications vs real golang_package: none beyond what's below —
// this is the one module in this batch with a cheap and accurate
// idempotency probe, unlike most package managers. Idempotency for
// present/absent is checked by testing for the binary file's existence
// in GOBIN (resolved via `go env GOBIN`, falling back to `go env
// GOPATH`+"/bin" the same way the real go toolchain itself does — the
// module's own note that GOBIN is "determined by go env GOBIN").
// state=latest always runs `go install pkg@latest` and reports changed
// (matching apt.go's "can't cheaply tell already-latest apart"
// convention) — and, since @latest by definition discards any version
// pin, ignores both `version` and an inline `@version` suffix for that
// state.
func moduleGolangPackage(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names, err := resolveNames(args)
	if err != nil {
		return Result{}, errArg("golang_package: %v", err)
	}
	state := argString(args, "state", "present")
	version := argString(args, "version", "")
	exe := argString(args, "executable", "go")

	if version != "" {
		if len(names) != 1 {
			return Result{}, errArg("golang_package: version can only be used with a single name")
		}
		if strings.Contains(names[0], "@") {
			return Result{}, errArg("golang_package: version cannot be combined with an inline @version suffix")
		}
	}

	gobin, err := golangPackageGobin(ctx, conn, exe)
	if err != nil {
		return Result{}, err
	}

	query := func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
		return pathExists(ctx, conn, gobin+"/"+golangPackageBinName(name))
	}
	install := func(ctx context.Context, conn remoteexec.Connection, names []string) error {
		for _, n := range names {
			target := n
			if !strings.Contains(target, "@") {
				if version != "" {
					target += "@" + version
				} else {
					target += "@latest"
				}
			}
			if _, err := run(ctx, conn, exe+" install "+shellQuote(target)); err != nil {
				return err
			}
		}
		return nil
	}
	remove := func(ctx context.Context, conn remoteexec.Connection, names []string) error {
		var paths []string
		for _, n := range names {
			paths = append(paths, shellQuote(gobin+"/"+golangPackageBinName(n)))
		}
		_, err := run(ctx, conn, "rm -f "+strings.Join(paths, " "))
		return err
	}
	latest := func(ctx context.Context, conn remoteexec.Connection, names []string) error {
		for _, n := range names {
			pkgPath, _, _ := strings.Cut(n, "@")
			if _, err := run(ctx, conn, exe+" install "+shellQuote(pkgPath+"@latest")); err != nil {
				return err
			}
		}
		return nil
	}

	return pkgManagerLoop(ctx, conn, names, state, query, install, remove, latest)
}

// golangPackageBinName returns the binary name `go install` would
// produce for pkg — the last path segment of the import path, ignoring
// any inline "@version" suffix.
func golangPackageBinName(pkg string) string {
	pkgPath, _, _ := strings.Cut(pkg, "@")
	if i := strings.LastIndex(pkgPath, "/"); i >= 0 {
		return pkgPath[i+1:]
	}
	return pkgPath
}

// golangPackageGobin resolves the target's GOBIN the same way the go
// toolchain itself does: GOBIN if set, else GOPATH+"/bin".
func golangPackageGobin(ctx context.Context, conn remoteexec.Connection, exe string) (string, error) {
	gobin, err := run(ctx, conn, exe+" env GOBIN")
	if err != nil {
		return "", err
	}
	if gobin != "" {
		return gobin, nil
	}
	gopath, err := run(ctx, conn, exe+" env GOPATH")
	if err != nil {
		return "", err
	}
	return gopath + "/bin", nil
}
