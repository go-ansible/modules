package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePipx implements (a subset of) Ansible's `pipx` module: manages
// Python applications installed in isolated virtualenvs via `pipx`.
//
// Args: state (default "install") — present/install (alias of each
// other), absent/uninstall (alias of each other), upgrade,
// upgrade_all, reinstall, inject, latest (equivalent to running
// install then upgrade, matching real pipx exactly); name (string,
// required for every supported state except upgrade_all); source
// (string, optional) — for install/latest, install FROM this (a VCS
// URL, local path, or version-specifier package spec) instead of
// plain `name`; force (bool, default false) — `--force`; index_url
// (string, optional) — `--index-url`; python (string, optional) —
// `--python`; system_site_packages (bool, default false) —
// `--system-site-packages`; editable (bool, default false) —
// `--editable`; pip_args (string, optional) — `--pip-args=<args>`;
// suffix (string, optional) — `--suffix`, and appended to name to form
// the installed application's own venv/executable name, matching real
// pipx exactly; install_deps (bool, default false) — `--include-deps`
// (install only); include_injected (bool, default false) —
// `--include-injected` (upgrade/upgrade_all/latest only);
// inject_packages ([]string, required for state=inject); install_apps
// (bool, default false) — `--include-apps` (inject only); global (bool,
// default false) — `--global`; executable (string, default "pipx").
//
// Simplifications vs real pipx: real pipx defaults executable to
// `python -m pipx` using Ansible's own resolved interpreter when
// unset; this port defaults to the bare `pipx` command on PATH
// instead (it has no facts-gathering step to resolve a control-node
// Python path against the target). Real pipx accepts version
// specifiers in `name` (e.g. "tox<4.0.0") and only reinstalls when the
// installed version no longer satisfies the specifier; this port does
// no specifier parsing — `name` is always the literal package/app
// name. install_all/uninstall_all/uninject/upgrade_shared/
// reinstall_all/pin/unpin are NOT implemented (errArg) — this batch's
// budget went to the states demonstrated in real pipx's own EXAMPLES.
// Idempotency for install/upgrade/reinstall/inject is checked via
// `pipx list --include-injected --json` (see pipxListApplications,
// shared with pipx_info.go); this port also runs `pipx version` first
// and fails cleanly if it is older than 1.7.0, matching real pipx's
// own version gate. Real pipx determines `changed` by diffing the
// full application list before and after (via its own
// StateModuleHelper framework); this port instead reports Changed
// whenever it actually runs a mutating pipx command (skipping the
// command entirely, and reporting Ok, only when install/uninstall's
// own pre-check already shows the desired state) — matching this
// batch's house "can't cheaply tell a no-op apart" convention for
// upgrade/reinstall/inject/upgrade_all specifically, since those have
// no cheap yes/no idempotency check of their own.
func modulePipx(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	exe := argString(args, "executable", "pipx")
	if exe == "" {
		exe = "pipx"
	}
	global := argBool(args, "global", false)

	if err := pipxCheckVersion(ctx, conn, exe, global); err != nil {
		return Result{}, err
	}

	state := argString(args, "state", "install")
	switch state {
	case "present", "install":
		return pipxInstall(ctx, conn, exe, global, args, false)
	case "absent", "uninstall":
		return pipxUninstall(ctx, conn, exe, global, args)
	case "upgrade":
		return pipxUpgrade(ctx, conn, exe, global, args)
	case "upgrade_all":
		return pipxUpgradeAll(ctx, conn, exe, global, args)
	case "reinstall":
		return pipxReinstall(ctx, conn, exe, global, args)
	case "inject":
		return pipxInject(ctx, conn, exe, global, args)
	case "latest":
		if _, err := pipxInstall(ctx, conn, exe, global, args, true); err != nil {
			return Result{}, err
		}
		return pipxUpgrade(ctx, conn, exe, global, args)
	case "install_all", "uninstall_all", "uninject", "upgrade_shared", "reinstall_all", "pin", "unpin":
		return Result{}, errArg("pipx: state=%q is not supported by this port", state)
	default:
		return Result{}, errArg("pipx: unrecognized state %q", state)
	}
}

func pipxInstallName(args map[string]any) (string, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return "", err
	}
	if suffix := argString(args, "suffix", ""); suffix != "" {
		name += suffix
	}
	return name, nil
}

func pipxCommonFlags(args map[string]any) string {
	flags := ""
	if v := argString(args, "index_url", ""); v != "" {
		flags += " --index-url " + shellQuote(v)
	}
	if argBool(args, "force", false) {
		flags += " --force"
	}
	if argBool(args, "editable", false) {
		flags += " --editable"
	}
	if v := argString(args, "pip_args", ""); v != "" {
		flags += " --pip-args=" + shellQuote(v)
	}
	return flags
}

func pipxInstall(ctx context.Context, conn remoteexec.Connection, exe string, global bool, args map[string]any, forLatest bool) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	installName, _ := pipxInstallName(args)
	force := argBool(args, "force", false)

	apps, err := pipxListApplications(ctx, conn, exe, global, true, false)
	if err != nil {
		return Result{}, err
	}
	if _, present := apps[installName]; present && !force {
		return Ok(installName + " already installed"), nil
	}

	sourceOrName := argString(args, "source", "")
	if sourceOrName == "" {
		sourceOrName = name
	}

	cmd := exe
	if global {
		cmd += " --global"
	}
	cmd += " install"
	cmd += pipxCommonFlags(args)
	if argBool(args, "install_deps", false) {
		cmd += " --include-deps"
	}
	if v := argString(args, "python", ""); v != "" {
		cmd += " --python " + shellQuote(v)
	}
	if argBool(args, "system_site_packages", false) {
		cmd += " --system-site-packages"
	}
	if suffix := argString(args, "suffix", ""); suffix != "" {
		cmd += " --suffix " + shellQuote(suffix)
	}
	cmd += " " + shellQuote(sourceOrName)

	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(installName + " installed"), nil
}

func pipxUninstall(ctx context.Context, conn remoteexec.Connection, exe string, global bool, args map[string]any) (Result, error) {
	installName, err := pipxInstallName(args)
	if err != nil {
		return Result{}, err
	}
	apps, err := pipxListApplications(ctx, conn, exe, global, false, false)
	if err != nil {
		return Result{}, err
	}
	if _, present := apps[installName]; !present {
		return Ok(installName + " already absent"), nil
	}
	cmd := exe
	if global {
		cmd += " --global"
	}
	cmd += " uninstall " + shellQuote(installName)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(installName + " removed"), nil
}

func pipxUpgrade(ctx context.Context, conn remoteexec.Connection, exe string, global bool, args map[string]any) (Result, error) {
	installName, err := pipxInstallName(args)
	if err != nil {
		return Result{}, err
	}
	apps, err := pipxListApplications(ctx, conn, exe, global, false, false)
	if err != nil {
		return Result{}, err
	}
	if _, present := apps[installName]; !present {
		return Fail("pipx: trying to upgrade a non-existent application: " + installName), nil
	}
	cmd := exe
	if global {
		cmd += " --global"
	}
	cmd += " upgrade"
	cmd += pipxCommonFlags(args)
	if argBool(args, "include_injected", false) {
		cmd += " --include-injected"
	}
	cmd += " " + shellQuote(installName)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(installName + " upgraded"), nil
}

func pipxUpgradeAll(ctx context.Context, conn remoteexec.Connection, exe string, global bool, args map[string]any) (Result, error) {
	cmd := exe
	if global {
		cmd += " --global"
	}
	cmd += " upgrade-all"
	if argBool(args, "include_injected", false) {
		cmd += " --include-injected"
	}
	if argBool(args, "force", false) {
		cmd += " --force"
	}
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed("upgraded all applications"), nil
}

func pipxReinstall(ctx context.Context, conn remoteexec.Connection, exe string, global bool, args map[string]any) (Result, error) {
	installName, err := pipxInstallName(args)
	if err != nil {
		return Result{}, err
	}
	apps, err := pipxListApplications(ctx, conn, exe, global, false, false)
	if err != nil {
		return Result{}, err
	}
	if _, present := apps[installName]; !present {
		return Fail("pipx: trying to reinstall a non-existent application: " + installName), nil
	}
	cmd := exe
	if global {
		cmd += " --global"
	}
	cmd += " reinstall"
	if v := argString(args, "python", ""); v != "" {
		cmd += " --python " + shellQuote(v)
	}
	cmd += " " + shellQuote(installName)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(installName + " reinstalled"), nil
}

func pipxInject(ctx context.Context, conn remoteexec.Connection, exe string, global bool, args map[string]any) (Result, error) {
	installName, err := pipxInstallName(args)
	if err != nil {
		return Result{}, err
	}
	injectPackages := argStringList(args, "inject_packages")
	if len(injectPackages) == 0 {
		return Result{}, errArg("pipx: inject_packages is required when state=inject")
	}
	apps, err := pipxListApplications(ctx, conn, exe, global, false, false)
	if err != nil {
		return Result{}, err
	}
	if _, present := apps[installName]; !present {
		return Fail("pipx: trying to inject packages into a non-existent application: " + installName), nil
	}
	cmd := exe
	if global {
		cmd += " --global"
	}
	cmd += " inject"
	cmd += pipxCommonFlags(args)
	if argBool(args, "install_apps", false) {
		cmd += " --include-apps"
	}
	if argBool(args, "install_deps", false) {
		cmd += " --include-deps"
	}
	cmd += " " + shellQuote(installName) + " " + quoteAll(injectPackages)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(installName + " injected"), nil
}

// pipxApp is one entry of `pipx list --json`'s own "venvs" map.
type pipxApp struct {
	Version      string
	Pinned       *bool
	Injected     map[string]string
	Dependencies []string
}

// pipxListApplications runs `pipx [--global] list --include-injected
// --json` and parses its "venvs" map into name -> pipxApp, matching
// real pipx/pipx_info's own shared _pipx.py:make_process_dict()
// exactly (including its ×injected/×deps population, controlled the
// same way by includeInjected/includeDeps here).
func pipxListApplications(ctx context.Context, conn remoteexec.Connection, exe string, global, includeInjected, includeDeps bool) (map[string]pipxApp, error) {
	out, err := pipxListJSON(ctx, conn, exe, global)
	if err != nil {
		return nil, err
	}
	return pipxParseList(out, includeInjected, includeDeps)
}

// pipxListJSON runs `pipx [--global] list --include-injected --json`
// and returns its raw stdout, shared by pipxListApplications and
// pipx_info.go's own include_raw handling so the command only runs
// once per module invocation.
func pipxListJSON(ctx context.Context, conn remoteexec.Connection, exe string, global bool) (string, error) {
	cmd := "USE_EMOJI=0 PIPX_USE_EMOJI=0 " + exe
	if global {
		cmd += " --global"
	}
	cmd += " list --include-injected --json"
	return run(ctx, conn, cmd)
}

func pipxParseList(out string, includeInjected, includeDeps bool) (map[string]pipxApp, error) {
	if out == "" {
		return map[string]pipxApp{}, nil
	}
	var raw struct {
		Venvs map[string]struct {
			Metadata struct {
				MainPackage struct {
					PackageVersion         string          `json:"package_version"`
					Pinned                 *bool           `json:"pinned"`
					AppPathsOfDependencies json.RawMessage `json:"app_paths_of_dependencies"`
				} `json:"main_package"`
				InjectedPackages map[string]struct {
					PackageVersion string `json:"package_version"`
				} `json:"injected_packages"`
			} `json:"metadata"`
		} `json:"venvs"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("pipx: parsing `pipx list` output: %w", err)
	}
	result := map[string]pipxApp{}
	for name, venv := range raw.Venvs {
		app := pipxApp{Version: venv.Metadata.MainPackage.PackageVersion, Pinned: venv.Metadata.MainPackage.Pinned}
		if includeInjected {
			app.Injected = map[string]string{}
			for pkg, v := range venv.Metadata.InjectedPackages {
				app.Injected[pkg] = v.PackageVersion
			}
		}
		if includeDeps {
			var deps map[string]any
			if json.Unmarshal(venv.Metadata.MainPackage.AppPathsOfDependencies, &deps) == nil {
				for dep := range deps {
					app.Dependencies = append(app.Dependencies, dep)
				}
				sort.Strings(app.Dependencies)
			}
		}
		result[name] = app
	}
	return result, nil
}

// pipxCheckVersion runs `pipx version` and fails cleanly if it can't
// be parsed as at least 1.7.0, matching real pipx/pipx_info's own
// LooseVersion gate.
func pipxCheckVersion(ctx context.Context, conn remoteexec.Connection, exe string, global bool) error {
	cmd := exe
	if global {
		cmd += " --global"
	}
	cmd += " version"
	out, err := run(ctx, conn, cmd)
	if err != nil {
		return err
	}
	if !pipxVersionAtLeast(out, 1, 7, 0) {
		return errArg("pipx: the pipx tool must be at least version 1.7.0, found %q", out)
	}
	return nil
}

func pipxVersionAtLeast(v string, major, minor, patch int) bool {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
	nums := make([]int, 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		field := parts[i]
		for j, r := range field {
			if r < '0' || r > '9' {
				field = field[:j]
				break
			}
		}
		n, err := strconv.Atoi(field)
		if err != nil {
			return false
		}
		nums[i] = n
	}
	want := [3]int{major, minor, patch}
	for i := 0; i < 3; i++ {
		if nums[i] != want[i] {
			return nums[i] > want[i]
		}
	}
	return true
}
