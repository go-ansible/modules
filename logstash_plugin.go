package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLogstashPlugin implements Ansible's `logstash_plugin`
// (community.general) module: installs or removes a Logstash plugin
// via the `logstash-plugin` CLI tool — read from real
// logstash_plugin.py's own is_plugin_present/install_plugin/
// remove_plugin functions (this batch's hard rule: the exact argv
// shape and idempotency check are only visible there, not EXAMPLES/
// OPTIONS).
//
// Args: name (required); state (present|absent, default present);
// plugin_bin (default "/usr/share/logstash/bin/logstash-plugin") —
// unlike elasticsearch_plugin.go's own esPluginFindBinary, real
// logstash_plugin never verifies this path exists beforehand; it is
// simply run, and a missing binary surfaces as a non-zero exit like
// any other command failure, which this port matches; proxy_host/
// proxy_port — set as the http_proxy/https_proxy environment variables
// on the install command only (never for the presence check or
// removal), matching real logstash_plugin's own install_plugin-only
// environ_update; if proxy_host contains "://" it is used verbatim as
// the scheme, otherwise "http://" is prepended (matching real
// logstash_plugin's own scheme detection); version — appended as
// `--version <v>` to the install command only if the plugin is not
// already present (see below).
//
// present = `<plugin_bin> list <name>` exits 0. Skipped (unchanged) if
// (present and state=present) or (state=absent and not present);
// otherwise runs `<plugin_bin> install [--version V] <name>` or
// `<plugin_bin> remove <name>`.
//
// Every command this module runs (including the presence check) is
// prefixed with LANGUAGE=C LC_ALL=C, matching real logstash_plugin's
// own module.run_command_environ_update — its own parse_error (the
// text after "reason: " in stdout, or all of stdout if absent) depends
// on Logstash's English-locale error wording.
//
// Extra: cmd (string — the space-joined argv, unlike
// elasticsearch_plugin.go's own []string, matching real
// logstash_plugin's own `" ".join(cmd_args)` return value literally),
// name, state, stdout, stderr.
//
// Deviation: real logstash_plugin runs its own module.run_command on
// the target directly; this port composes the same argv (LANGUAGE=C
// LC_ALL=C prefix included) and runs it through conn.Exec's single
// command string instead, matching this package's own architecture
// (see module.go's own doc comment).
func moduleLogstashPlugin(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("logstash_plugin: state must be present or absent, got %q", state)
	}
	pluginBin := argString(args, "plugin_bin", "/usr/share/logstash/bin/logstash-plugin")
	proxyHost := argString(args, "proxy_host", "")
	proxyPort := argString(args, "proxy_port", "")
	version := argString(args, "version", "")

	listRes, err := runStatus(ctx, conn, logstashPluginCmd(pluginBin, []string{"list", name}))
	if err != nil {
		return Result{}, err
	}
	present := listRes.RC == 0

	if (present && state == "present") || (state == "absent" && !present) {
		return Ok("").WithExtra("name", name).WithExtra("state", state), nil
	}

	var cmdArgs []string
	var envPrefix string
	if state == "present" {
		cmdArgs = []string{pluginBin, "install"}
		if version != "" {
			cmdArgs = append(cmdArgs, "--version", version)
		}
		cmdArgs = append(cmdArgs, name)
		if proxyHost != "" && proxyPort != "" {
			scheme := proxyHost
			if !strings.Contains(scheme, "://") {
				scheme = "http://" + scheme
			}
			proxyURL := scheme + ":" + proxyPort
			envPrefix = "http_proxy=" + shellQuote(proxyURL) + " https_proxy=" + shellQuote(proxyURL) + " "
		}
	} else {
		cmdArgs = []string{pluginBin, "remove", name}
	}

	cmd := envPrefix + logstashPluginCmd(pluginBin, cmdArgs[1:])
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(logstashPluginParseError(res.Stdout)).WithExtra("stderr", res.Stderr), nil
	}

	cmdStr := strings.Join(cmdArgs, " ")
	r := Changed("").WithExtra("cmd", cmdStr).WithExtra("name", name).WithExtra("state", state).
		WithExtra("stdout", res.Stdout).WithExtra("stderr", res.Stderr)
	return r, nil
}

// logstashPluginCmd builds `LANGUAGE=C LC_ALL=C <bin> <rest...>`,
// matching real logstash_plugin's own global environ_update.
func logstashPluginCmd(bin string, rest []string) string {
	argv := append([]string{bin}, rest...)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return "LANGUAGE=C LC_ALL=C " + strings.Join(quoted, " ")
}

// logstashPluginParseError matches real logstash_plugin.py's own
// parse_error: the text after the first "reason: " in s, or s itself
// if no such marker is present.
func logstashPluginParseError(s string) string {
	const marker = "reason: "
	if idx := strings.Index(s, marker); idx >= 0 {
		return strings.TrimSpace(s[idx+len(marker):])
	}
	return s
}
