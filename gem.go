package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGem implements (a subset of) Ansible's `gem` module: manages
// Ruby gems via `gem install`/`gem uninstall`.
//
// Args: name (string, required); state (present|absent|latest, default
// "present"); version (string, optional); user_install (bool, default
// true) — passes `--user-install`/`--no-user-install`; executable
// (string, optional path to the `gem` binary).
//
// Simplifications vs real gem: no `bindir`, `build_flags`,
// `env_shebang`, `force`, `gem_source`, `include_dependencies`,
// `include_doc`, `install_dir`, `norc`, or
// `override_platform_install_dir` support. Idempotency for
// present/absent is checked via `gem list -i <name>[-v version]` (exit
// 0 iff installed at that version, or any version when unpinned);
// state=latest always runs `gem update <name>` and reports changed,
// matching apt.go's "can't cheaply tell already-latest apart"
// convention.
func moduleGem(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	version := argString(args, "version", "")
	userInstall := argBool(args, "user_install", true)
	gemBin := argString(args, "executable", "gem")

	userFlag := " --no-user-install"
	if userInstall {
		userFlag = " --user-install"
	}
	versionFlag := ""
	if version != "" {
		versionFlag = " -v " + shellQuote(version)
	}

	switch state {
	case "absent":
		installed, err := gemInstalled(ctx, conn, gemBin, name, version)
		if err != nil {
			return Result{}, err
		}
		if !installed {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, gemBin+" uninstall"+versionFlag+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil

	case "latest":
		if _, err := run(ctx, conn, gemBin+" update"+userFlag+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " updated"), nil

	default: // "present"
		installed, err := gemInstalled(ctx, conn, gemBin, name, version)
		if err != nil {
			return Result{}, err
		}
		if installed {
			return Ok(name + " already installed"), nil
		}
		if _, err := run(ctx, conn, gemBin+" install"+versionFlag+userFlag+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " installed"), nil
	}
}

func gemInstalled(ctx context.Context, conn remoteexec.Connection, gemBin, name, version string) (bool, error) {
	cmd := gemBin + " list -i " + shellQuote(name)
	if version != "" {
		cmd += " -v " + shellQuote(version)
	}
	res, err := conn.Exec(ctx, cmd+" >/dev/null 2>&1", nil)
	if err != nil {
		return false, fmt.Errorf("checking gem %s: %w", name, err)
	}
	return res.RC == 0, nil
}
