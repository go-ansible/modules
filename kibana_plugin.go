package modules

import (
	"context"
	"path"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKibanaPlugin implements Ansible's `kibana_plugin`
// (community.general) module: installs or removes a Kibana plugin —
// read from real kibana_plugin.py's own install_plugin/remove_plugin/
// get_kibana_version/is_plugin_present functions (this batch's hard
// rule: the exact argv shape, the version-gated binary switch, and the
// idempotency check are only visible there, not EXAMPLES/OPTIONS).
//
// Args: name (required); state (present|absent, default present); url
// (a source URL, or a local "file://" path — install only); timeout
// (default "1m" — install only); plugin_bin (default
// "/opt/kibana/bin/kibana" — the KIBANA binary; see below for how the
// PLUGIN binary is derived from it); plugin_dir (default
// "/opt/kibana/installedPlugins/"); version (appended as "name/version"
// to the plugin name for BOTH install and remove, unlike
// elasticsearch_plugin.go's own version handling); force (bool,
// default false) — when true, always removes any existing plugin
// first (ignoring its result) before installing, and bypasses the
// present/absent skip check entirely; allow_root (bool, default
// false) — `--allow-root`, passed to every command this module runs
// including the version probe below.
//
// Binary resolution: this module first runs `<plugin_bin> --version
// [--allow-root]` (fail if non-zero) to learn the installed Kibana
// version, matching real kibana_plugin's own get_kibana_version. If
// that version compares greater than "4.6" (true for every Kibana
// release this port is likely to see; a real dotted-version compare,
// not a string compare, matching Python's own LooseVersion), install/
// remove run against `<dir of plugin_bin>/kibana-plugin install|remove
// <name>`; otherwise (legacy Kibana <= 4.6) they run against
// `<plugin_bin> plugin --install|--remove <name> [--url U]` — the
// --url flag is only ever sent on that legacy path, matching real
// kibana_plugin's own install_plugin exactly (a real limitation of the
// upstream module: --url has no effect against modern kibana-plugin).
//
// present = a directory named parse_plugin_repo(name) (see
// elasticsearch_plugin.go's own esParsePluginRepo, the same parsing
// real kibana_plugin.py duplicates) exists under plugin_dir, checked
// BEFORE version is appended to name. Skipped (unchanged) if (present
// and state=present and not force) or (state=absent and not present
// and not force).
//
// A non-zero exit fails with real kibana_plugin's own parse_error (the
// text after the first "reason: " in stdout, or all of stdout if
// absent — the same wording logstash_plugin.go's own
// logstashPluginParseError implements, shared here as the same helper
// since both real modules' own parse_error functions are byte-for-byte
// identical).
//
// Extra: cmd (string — the space-joined argv actually run), name,
// state, url, timeout.
//
// Deviation: real kibana_plugin runs its own module.run_command on the
// target directly, with LANGUAGE=C LC_ALL=C set globally (matching
// logstash_plugin.go's own logstashPluginCmd prefix, reused here for
// the same reason); this port composes the same argv and runs it
// through conn.Exec's single command string instead, matching this
// package's own architecture (see module.go's own doc comment).
func moduleKibanaPlugin(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("kibana_plugin: state must be present or absent, got %q", state)
	}
	url := argString(args, "url", "")
	timeout := argString(args, "timeout", "1m")
	pluginBin := argString(args, "plugin_bin", "/opt/kibana/bin/kibana")
	pluginDir := argString(args, "plugin_dir", "/opt/kibana/installedPlugins/")
	version := argString(args, "version", "")
	force := argBool(args, "force", false)
	allowRoot := argBool(args, "allow_root", false)

	verArgv := []string{pluginBin, "--version"}
	if allowRoot {
		verArgv = append(verArgv, "--allow-root")
	}
	verRes, err := conn.Exec(ctx, logstashPluginCmd(verArgv[0], verArgv[1:]), nil)
	if err != nil {
		return Result{}, err
	}
	if verRes.RC != 0 {
		return Fail("Failed to get Kibana version : " + strings.TrimSpace(verRes.Stderr)), nil
	}
	kibanaVersion := strings.TrimSpace(verRes.Stdout)
	modern := looseVersionGreater(kibanaVersion, "4.6")

	repo := esParsePluginRepo(name)
	present, err := pathIsDir(ctx, conn, joinRemotePath(pluginDir, repo))
	if err != nil {
		return Result{}, err
	}

	if (present && state == "present" && !force) || (state == "absent" && !present && !force) {
		return Ok("").WithExtra("name", name).WithExtra("state", state), nil
	}

	target := name
	if version != "" {
		target = name + "/" + version
	}

	if state == "present" && force {
		// Result intentionally ignored: matches real kibana_plugin's
		// own force branch, which discards remove_plugin's return
		// value entirely before installing.
		_, _ = kibanaRunPluginCmd(ctx, conn, kibanaRemoveArgv(pluginBin, target, allowRoot, modern))
	}

	var cmdArgs []string
	if state == "present" {
		cmdArgs = kibanaInstallArgv(pluginBin, target, url, timeout, allowRoot, modern)
	} else {
		cmdArgs = kibanaRemoveArgv(pluginBin, target, allowRoot, modern)
	}

	res, err := kibanaRunPluginCmd(ctx, conn, cmdArgs)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(logstashPluginParseError(res.Stdout)), nil
	}

	cmdStr := strings.Join(cmdArgs, " ")
	r := Changed("").WithExtra("cmd", cmdStr).WithExtra("name", name).WithExtra("state", state).
		WithExtra("url", url).WithExtra("timeout", timeout)
	return r, nil
}

func kibanaRunPluginCmd(ctx context.Context, conn remoteexec.Connection, argv []string) (remoteexec.Result, error) {
	return conn.Exec(ctx, logstashPluginCmd(argv[0], argv[1:]), nil)
}

func kibanaInstallArgv(pluginBin, target, url, timeout string, allowRoot, modern bool) []string {
	var argv []string
	if modern {
		argv = []string{kibanaPluginBinFrom(pluginBin), "install"}
		if url != "" {
			argv = append(argv, url)
		} else {
			argv = append(argv, target)
		}
	} else {
		argv = []string{pluginBin, "plugin", "--install", target}
		if url != "" {
			argv = append(argv, "--url", url)
		}
	}
	if timeout != "" {
		argv = append(argv, "--timeout", timeout)
	}
	if allowRoot {
		argv = append(argv, "--allow-root")
	}
	return argv
}

func kibanaRemoveArgv(pluginBin, target string, allowRoot, modern bool) []string {
	var argv []string
	if modern {
		argv = []string{kibanaPluginBinFrom(pluginBin), "remove", target}
	} else {
		argv = []string{pluginBin, "plugin", "--remove", target}
	}
	if allowRoot {
		argv = append(argv, "--allow-root")
	}
	return argv
}

// kibanaPluginBinFrom matches real kibana_plugin's own
// os.path.join(os.path.dirname(plugin_bin), "kibana-plugin").
func kibanaPluginBinFrom(pluginBin string) string {
	return path.Join(path.Dir(pluginBin), "kibana-plugin")
}

// looseVersionGreater reports whether a > b, comparing dot-separated
// numeric segments left to right the way Python's own list/tuple
// comparison does for distutils.version.LooseVersion: when one
// version is a strict prefix of the other's segments (e.g. "4.6" vs
// "4.6.0"), the LONGER one compares greater, regardless of what its
// extra trailing segment's value is — "4.6.0" > "4.6" is true in
// Python for the same reason (4, 6, 0) > (4, 6) is true: a shorter
// tuple compares less than any tuple sharing its full prefix, not as
// if padded with zeros. A non-numeric segment compares as 0.
func looseVersionGreater(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		if i >= len(as) {
			return false
		}
		if i >= len(bs) {
			return true
		}
		av, _ := strconv.Atoi(strings.TrimSpace(as[i]))
		bv, _ := strconv.Atoi(strings.TrimSpace(bs[i]))
		if av != bv {
			return av > bv
		}
	}
	return false
}
