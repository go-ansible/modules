package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleEasyInstall implements (a subset of) Ansible's `easy_install`
// module: installs a Python library via the legacy `easy_install`
// command (setuptools' own old installer), optionally inside a
// `virtualenv` it creates first.
//
// Args: name (string, required); state (present|latest, default
// "present") — "latest" adds `--upgrade`; executable (string, default
// "easy_install") — the easy_install binary to run, preferring
// `<virtualenv>/bin/<executable>` when virtualenv is set and that path
// exists; virtualenv (string, optional) — a directory to create (if
// `<virtualenv>/bin/activate` does not already exist) and install into;
// virtualenv_command (string, default "virtualenv") — the command used
// to create it (e.g. "pyvenv", "virtualenv2"); virtualenv_site_packages
// (bool, default false) — adds `--system-site-packages` to the creation
// command (real easy_install.py's own documented no-op-on-an-
// already-existing-venv caveat applies here too, since this port only
// ever creates a virtualenv that doesn't already exist).
//
// Real easy_install can only INSTALL — it has no `absent` state
// (matching this port's own choice list: present/latest only), and this
// port's own idempotency check matches real easy_install.py's own
// technique exactly: `easy_install --dry-run [--upgrade] <name>`, and
// grep its output for the literal string "Downloading" — real
// easy_install always prints that when it would actually fetch
// something, so its ABSENCE means "already satisfies this request",
// even for `state: latest` (a `--dry-run --upgrade` that finds a newer
// version available still prints "Downloading"). Recent setuptools
// releases have REMOVED the easy_install script entirely (not merely
// deprecated it) — on such a target this port's own `command -v
// easy_install` (or the virtualenv-relative path) check fails loud, the
// same as any other missing-binary case elsewhere in this port; no
// special-cased handling is added for that beyond the normal
// binary-presence check.
func moduleEasyInstall(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, errArg("easy_install: %v", err)
	}
	state := argString(args, "state", "present")
	executable := argString(args, "executable", "easy_install")
	venv := argString(args, "virtualenv", "")
	venvCommand := argString(args, "virtualenv_command", "virtualenv")
	venvSitePackages := argBool(args, "virtualenv_site_packages", false)

	if venv != "" {
		hasActivate, err := pathExists(ctx, conn, venv+"/bin/activate")
		if err != nil {
			return Result{}, err
		}
		if !hasActivate {
			cmd := venvCommand + " " + shellQuote(venv)
			if venvSitePackages {
				cmd += " --system-site-packages"
			}
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
		}
	}

	easyInstall := executable
	if venv != "" {
		candidate := venv + "/bin/" + executable
		exists, err := pathExists(ctx, conn, candidate)
		if err != nil {
			return Result{}, err
		}
		if exists {
			easyInstall = candidate
		}
	}
	if easyInstall == executable {
		if _, err := run(ctx, conn, "command -v "+shellQuote(easyInstall)); err != nil {
			return Result{}, fmt.Errorf("easy_install: %s not found on PATH: %w", easyInstall, err)
		}
	}

	extraArgs := ""
	if state == "latest" {
		extraArgs = " --upgrade"
	}

	installed, err := easyInstallSatisfied(ctx, conn, easyInstall, extraArgs, name)
	if err != nil {
		return Result{}, err
	}
	if installed {
		return Ok(name+" already satisfies request").WithExtra("binary", easyInstall).WithExtra("virtualenv", venv), nil
	}

	if _, err := run(ctx, conn, shellQuote(easyInstall)+extraArgs+" "+shellQuote(name)); err != nil {
		return Result{}, err
	}
	return Changed(name+" installed").WithExtra("binary", easyInstall).WithExtra("virtualenv", venv), nil
}

// easyInstallSatisfied runs `<easyInstall> --dry-run <extraArgs>
// <name>` and reports true (already satisfied, no install needed) iff
// its combined output does NOT contain the literal string
// "Downloading" — see moduleEasyInstall's own doc comment for why this
// mirrors real easy_install.py's own technique exactly.
func easyInstallSatisfied(ctx context.Context, conn remoteexec.Connection, easyInstall, extraArgs, name string) (bool, error) {
	cmd := shellQuote(easyInstall) + extraArgs + " --dry-run " + shellQuote(name) + " 2>&1"
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return false, fmt.Errorf("checking easy_install package %s: %w", name, err)
	}
	if res.RC != 0 {
		return false, fmt.Errorf("easy_install --dry-run %s: exit %d: %s", name, res.RC, strings.TrimSpace(res.Stdout))
	}
	return !strings.Contains(res.Stdout, "Downloading"), nil
}
