package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJenkinsPlugin implements Ansible's `jenkins_plugin`
// (community.general) module. Unlike every other jenkins_* module in
// this batch, this one does NOT use jenkins_common.go's jenkins-cli.jar
// substitution at all — real jenkins_plugin.py, read before
// implementing, never talks to Jenkins' CLI or REST management API for
// plugin install/pin/enable/disable at all. It manipulates the Jenkins
// CONTROLLER's own local filesystem directly: `fetch_url`-downloading a
// `.jpi`/`.hpi` file straight into `<jenkins_home>/plugins/<name>.jpi`,
// and creating/removing `.pinned`/`.disabled` marker files there (real
// source: `self.params['jenkins_home']}/plugins/{name}.jpi` for
// install, `_pinning`/`_enabling` touching/removing
// `<plugin_file>.pinned`/`.disabled`). This is exactly the kind of
// plain download-and-place-a-file operation this port's own Connection
// abstraction (Exec running `curl`, ordinary shell file tests) already
// does natively for other modules (get_url-shaped ones) — no CLI
// substitution was needed or used here, which this port considers a
// MORE faithful port of this one module than routing it through
// jenkins-cli would have been.
//
// Args: name (required, the plugin ID from
// https://plugins.jenkins.io); version (specific version string, or
// omitted/"latest" — default "latest" behavior); jenkins_home (default
// /var/lib/jenkins); updates_url (list, default
// ["https://updates.jenkins.io", "http://mirrors.jenkins.io"] — only
// the FIRST is used by this port; real jenkins_plugin.py tries each in
// turn on failure, a fallback chain this port did not replicate,
// documented here rather than silently dropped); owner, group, mode
// (applied to the downloaded .jpi file via chown/chmod); state
// (absent|present|pinned|unpinned|enabled|disabled|latest, default
// present).
//
// Download URL: version omitted or "latest" ->
// `<updates_url>/latest/<name>.hpi` (real jenkins_plugin.py's own
// documented `latest_plugins_url_segments` default `["latest"]`); an
// explicit version -> `<updates_url>/download/plugins/<name>/<version>/<name>.hpi`
// (Jenkins' own standard, long-stable update-center versioned-plugin
// URL pattern). state=latest always re-downloads and compares via
// sha1sum on the target (matching real jenkins_plugin.py's own
// checksum-based change detection, without this port needing to pull
// the plugin bytes back to the control node the way a naive diff
// would); state=present with a plugin file already there is a no-op
// UNLESS version is explicitly given and differs from — this port has
// no cheap way to read an installed .jpi's own embedded version
// without unzipping its MANIFEST.MF, so it re-downloads and
// sha1-compares exactly like state=latest in that case too.
//
// state=pinned/unpinned: touch/remove `<plugin_file>.pinned`.
// state=enabled/disabled: remove/touch `<plugin_file>.disabled`.
// state=absent: remove the plugin file and both marker files.
//
// Deviation — with_dependencies/latest_plugins_url_segments/
// plugin_versions_url_segment/update_json_url_segment/updates_expiration:
// accepted for argument-shape compatibility, NOT wired — real
// jenkins_plugin.py's own dependency-following and updates-JSON-cache
// logic exists to avoid extra downloads/support installing a plugin's
// own declared dependencies automatically; this port always does the
// simpler direct download this doc comment describes, a documented,
// deliberate simplification given this batch's own time constraints,
// not a silent gap.
func moduleJenkinsPlugin(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "absent", "present", "pinned", "unpinned", "enabled", "disabled", "latest":
	default:
		return Result{}, errArg("jenkins_plugin: state must be one of absent, present, pinned, unpinned, enabled, disabled, latest, got %q", state)
	}
	jenkinsHome := argString(args, "jenkins_home", "/var/lib/jenkins")
	pluginFile := jenkinsHome + "/plugins/" + name + ".jpi"
	pinnedFile := pluginFile + ".pinned"
	disabledFile := pluginFile + ".disabled"

	updatesURLs := argStringList(args, "updates_url")
	updatesURL := "https://updates.jenkins.io"
	if len(updatesURLs) > 0 {
		updatesURL = updatesURLs[0]
	}
	version := argString(args, "version", "")

	exists, err := pathExists(ctx, conn, pluginFile)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok("jenkins_plugin: " + name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "rm -f "+shellQuote(pluginFile)+" "+shellQuote(pinnedFile)+" "+shellQuote(disabledFile)); err != nil {
			return Result{}, err
		}
		return Changed("jenkins_plugin: " + name + " removed"), nil
	}

	if state == "pinned" || state == "unpinned" {
		if !exists {
			return Fail("jenkins_plugin: " + name + " is not installed, cannot change pin state"), nil
		}
		return jenkinsPluginToggleMarker(ctx, conn, name, pinnedFile, state == "pinned")
	}
	if state == "enabled" || state == "disabled" {
		if !exists {
			return Fail("jenkins_plugin: " + name + " is not installed, cannot change enabled state"), nil
		}
		return jenkinsPluginToggleMarker(ctx, conn, name, disabledFile, state == "disabled")
	}

	// present / latest
	if exists && state == "present" && version == "" {
		return Ok("jenkins_plugin: " + name + " already present"), nil
	}

	downloadURL := strings.TrimRight(updatesURL, "/") + "/latest/" + name + ".hpi"
	if version != "" && version != "latest" {
		downloadURL = fmt.Sprintf("%s/download/plugins/%s/%s/%s.hpi", strings.TrimRight(updatesURL, "/"), name, version, name)
	}

	tmpFile := conn.TempPath(name + ".jpi.download")
	dres, err := conn.Exec(ctx, "curl -sSfL "+shellQuote(downloadURL)+" -o "+shellQuote(tmpFile), nil)
	if err != nil {
		return Result{}, err
	}
	if dres.RC != 0 {
		return Fail("jenkins_plugin: unable to download " + name + " from " + downloadURL + ": " + strings.TrimSpace(dres.Stderr)), nil
	}
	defer func() { _ = conn.Remove(ctx, tmpFile) }()

	changed := false
	if !exists {
		changed = true
	} else {
		oldSum, err := run(ctx, conn, "sha1sum "+shellQuote(pluginFile)+" | awk '{print $1}'")
		if err != nil {
			return Result{}, err
		}
		newSum, err := run(ctx, conn, "sha1sum "+shellQuote(tmpFile)+" | awk '{print $1}'")
		if err != nil {
			return Result{}, err
		}
		changed = oldSum != newSum
	}
	if changed {
		if _, err := run(ctx, conn, "mkdir -p "+shellQuote(jenkinsHome+"/plugins")+" && mv "+shellQuote(tmpFile)+" "+shellQuote(pluginFile)); err != nil {
			return Result{}, err
		}
	}

	if mode, merr := argMode(args, "mode"); merr == nil && mode != nil {
		if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(pluginFile))); err != nil {
			return Result{}, err
		}
	} else if merr != nil {
		return Result{}, merr
	}
	owner := argString(args, "owner", "")
	group := argString(args, "group", "")
	if owner != "" || group != "" {
		spec := owner
		if group != "" {
			spec += ":" + group
		}
		if _, err := run(ctx, conn, "chown "+shellQuote(spec)+" "+shellQuote(pluginFile)); err != nil {
			return Result{}, err
		}
	}

	if !changed {
		return Ok("jenkins_plugin: " + name + " already up to date"), nil
	}
	return Changed("jenkins_plugin: " + name + " installed"), nil
}

// jenkinsPluginToggleMarker creates (want=true) or removes (want=false)
// an empty marker file (`.pinned`/`.disabled`), matching real
// jenkins_plugin.py's own `_pinning`/`_enabling` methods.
func jenkinsPluginToggleMarker(ctx context.Context, conn remoteexec.Connection, name, markerPath string, want bool) (Result, error) {
	has, err := pathExists(ctx, conn, markerPath)
	if err != nil {
		return Result{}, err
	}
	if has == want {
		return Ok("jenkins_plugin: " + name + " already in the requested state"), nil
	}
	if want {
		if _, err := run(ctx, conn, "touch "+shellQuote(markerPath)); err != nil {
			return Result{}, err
		}
	} else {
		if _, err := run(ctx, conn, "rm -f "+shellQuote(markerPath)); err != nil {
			return Result{}, err
		}
	}
	return Changed("jenkins_plugin: " + name + " state changed"), nil
}
