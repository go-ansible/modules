package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleElasticsearchPlugin implements Ansible's `elasticsearch_plugin`
// (community.general) module: installs or removes an Elasticsearch
// plugin via the `elasticsearch-plugin` (or legacy `plugin`) CLI tool —
// read from real elasticsearch_plugin.py's own install_plugin/
// remove_plugin/get_plugin_bin/parse_plugin_repo functions (this
// batch's hard rule: the exact argv shape and idempotency check are
// only visible there, not EXAMPLES/OPTIONS).
//
// Args: name (required); state (present|absent, default present); src
// (a `file://` URL or remote URL, mutually exclusive with url); url
// (ES 1.x only, mutually exclusive with src); timeout (default "1m",
// only sent when the resolved binary is the legacy `plugin` command —
// see below); force (bool, default false) — `--batch`; plugin_bin (an
// explicit executable path, must exist as a file on the target);
// plugin_dir (default "/usr/share/elasticsearch/plugins/"); proxy_host/
// proxy_port — set as CLI_JAVA_OPTS/ES_JAVA_OPTS environment variables
// on the command, matching real elasticsearch_plugin's own
// run_command_environ_update; version — like timeout, only honored
// against the legacy `plugin` binary (appended as "name/version" to
// the install target); against the modern `elasticsearch-plugin`
// binary it is accepted but has NO effect, matching real
// elasticsearch_plugin's own is_old_command gate exactly (a real
// limitation of the upstream module, not a simplification introduced
// by this port).
//
// Binary resolution: an explicit plugin_bin must exist as a file
// (`test -f`) on the target or the module fails; otherwise this port
// probes /usr/share/elasticsearch/bin/elasticsearch-plugin then
// /usr/share/elasticsearch/bin/plugin, in that order, matching real
// elasticsearch_plugin's own PLUGIN_BIN_PATHS search order (this port
// omits its more elaborate PATH-plus-opt_dirs search via
// get_bin_path, since Connection has no equivalent primitive — those
// two canonical paths cover the documented usage).
//
// repo = parse_plugin_repo(name): the last "/"-separated element of
// name with any "elasticsearch-"/"es-" prefix stripped (matching real
// elasticsearch_plugin's own parsing of "username/pluginname" forms).
// present = a directory named repo exists under plugin_dir. Skipped
// (unchanged) if (present and state=present) or (state=absent and not
// present); otherwise runs `<bin> install [--timeout T] [--url U]
// [--batch] <src-or-name[/version]>` or `<bin> remove <repo>`.
//
// A non-zero exit fails with real elasticsearch_plugin's own
// parse_error: the text after "ERROR: " in stdout if present, else the
// whole of stdout.
//
// Extra: cmd ([]string), name, state, url, timeout, stdout, stderr.
//
// Deviation: real elasticsearch_plugin runs its own module.run_command
// on the target directly (a Python subprocess call after the module
// script was copied there); this port composes the same argv and runs
// it through conn.Exec's single command string instead, matching this
// package's own architecture (see module.go's own doc comment).
func moduleElasticsearchPlugin(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("elasticsearch_plugin: state must be present or absent, got %q", state)
	}
	src := argString(args, "src", "")
	url := argString(args, "url", "")
	if src != "" && url != "" {
		return Result{}, errArg("elasticsearch_plugin: src and url are mutually exclusive")
	}
	timeout := argString(args, "timeout", "1m")
	force := argBool(args, "force", false)
	pluginDir := argString(args, "plugin_dir", "/usr/share/elasticsearch/plugins/")
	proxyHost := argString(args, "proxy_host", "")
	proxyPort := argString(args, "proxy_port", "")
	version := argString(args, "version", "")

	bin, isOldCommand, failMsg, err := esPluginFindBinary(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	repo := esParsePluginRepo(name)
	present, err := pathIsDir(ctx, conn, joinRemotePath(pluginDir, repo))
	if err != nil {
		return Result{}, err
	}

	if (present && state == "present") || (state == "absent" && !present) {
		return Ok("").WithExtra("name", name).WithExtra("state", state), nil
	}

	var argv []string
	if state == "present" {
		argv = []string{bin, "install"}
		if isOldCommand && timeout != "" {
			argv = append(argv, "--timeout", timeout)
		}
		target := name
		if isOldCommand && version != "" {
			target = name + "/" + version
		}
		if url != "" {
			argv = append(argv, "--url", url)
		}
		if force {
			argv = append(argv, "--batch")
		}
		if src != "" {
			argv = append(argv, src)
		} else {
			argv = append(argv, target)
		}
	} else {
		argv = []string{bin, "remove", repo}
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := strings.Join(quoted, " ")
	if proxyHost != "" && proxyPort != "" {
		javaOpts := fmt.Sprintf("-Dhttp.proxyHost=%s -Dhttp.proxyPort=%s -Dhttps.proxyHost=%s -Dhttps.proxyPort=%s",
			proxyHost, proxyPort, proxyHost, proxyPort)
		cmd = "CLI_JAVA_OPTS=" + shellQuote(javaOpts) + " ES_JAVA_OPTS=" + shellQuote(javaOpts) + " " + cmd
	}

	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		verb := "Installing"
		if state == "absent" {
			verb = "Removing"
		}
		reason := esPluginParseError(res.Stdout)
		return Fail(fmt.Sprintf("%s plugin '%s' failed: %s", verb, name, reason)), nil
	}

	r := Changed("").WithExtra("cmd", argv).WithExtra("name", name).WithExtra("state", state).
		WithExtra("url", url).WithExtra("timeout", timeout).
		WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr)
	return r, nil
}

// esParsePluginRepo matches real elasticsearch_plugin.py's own
// parse_plugin_repo: the last "/"-separated element of s, with a
// leading "elasticsearch-" or "es-" prefix stripped.
func esParsePluginRepo(s string) string {
	elements := strings.Split(s, "/")
	repo := elements[0]
	if len(elements) > 1 {
		repo = elements[1]
	}
	for _, prefix := range []string{"elasticsearch-", "es-"} {
		if strings.HasPrefix(repo, prefix) {
			return repo[len(prefix):]
		}
	}
	return repo
}

// esPluginParseError matches real elasticsearch_plugin.py's own
// parse_error: the text after the first "ERROR: " in s, or s itself if
// no such marker is present.
func esPluginParseError(s string) string {
	const marker = "ERROR: "
	if idx := strings.Index(s, marker); idx >= 0 {
		return strings.TrimSpace(s[idx+len(marker):])
	}
	return s
}

// esPluginFindBinary resolves the elasticsearch-plugin executable —
// see moduleElasticsearchPlugin's own doc comment for the search order
// and the legacy-vs-modern distinction isOldCommand drives.
func esPluginFindBinary(ctx context.Context, conn remoteexec.Connection, args map[string]any) (bin string, isOldCommand bool, failMsg string, err error) {
	explicit := argString(args, "plugin_bin", "")
	if explicit != "" {
		ok, err := runTestFlag(ctx, conn, "-f", explicit)
		if err != nil {
			return "", false, "", err
		}
		if ok {
			return explicit, strings.HasSuffix(explicit, "/plugin"), "", nil
		}
		return "", false, fmt.Sprintf("%s does not exist and no other valid plugin installers were found. Make sure Elasticsearch is installed.", explicit), nil
	}
	for _, p := range []string{"/usr/share/elasticsearch/bin/elasticsearch-plugin", "/usr/share/elasticsearch/bin/plugin"} {
		ok, err := runTestFlag(ctx, conn, "-f", p)
		if err != nil {
			return "", false, "", err
		}
		if ok {
			return p, strings.HasSuffix(p, "/plugin"), "", nil
		}
	}
	return "", false, "None does not exist and no other valid plugin installers were found. Make sure Elasticsearch is installed.", nil
}

// runTestFlag runs `test <flag> <path>` on the target.
func runTestFlag(ctx context.Context, conn remoteexec.Connection, flag, path string) (bool, error) {
	res, err := runStatus(ctx, conn, "test "+flag+" "+shellQuote(path))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// pathIsDir reports whether path exists and is a directory on the
// target.
func pathIsDir(ctx context.Context, conn remoteexec.Connection, path string) (bool, error) {
	return runTestFlag(ctx, conn, "-d", path)
}

// joinRemotePath joins a directory and a name with exactly one slash
// between them, regardless of whether dir already ends in one —
// matching Python's os.path.join(plugin_dir, plugin_name) for the
// simple (no ".."/absolute-name) case every real *_plugin module here
// uses it for.
func joinRemotePath(dir, name string) string {
	if strings.HasSuffix(dir, "/") {
		return dir + name
	}
	return dir + "/" + name
}
