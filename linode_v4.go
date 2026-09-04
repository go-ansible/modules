package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLinodeV4 implements Ansible's `linode_v4` (community.general)
// module: creates or deletes a Linode instance via the Linode v4 API —
// see linode.go's own doc comment for why `linode` (this module's
// OLDER sibling, targeting Linode's now permanently-retired v3 API)
// and this module are NOT duplicates and are handled completely
// differently in this port.
//
// This port shells out to Linode's own official CLI, `linode-cli`
// (github.com/linode/linode-cli — the CURRENT one; there is also an
// older, explicitly-deprecated `linode/cli` repo whose own README
// says "Use https://github.com/linode/linode-cli instead", confirmed
// during this batch's own research, so this port targets the right
// one), instead of the `linode_api4` Python SDK real linode_v4.py uses
// — the same "shell out to the platform's own official CLI instead of
// an API client" precedent this port already uses elsewhere in this
// batch (see hwc_common.go/jenkins_common.go/packet_common.go's own
// doc comments).
//
// # Auth precondition and secrets
//
// `linode-cli` must already be configured on the target (a prior
// `linode-cli configure`, which writes ~/.config/linode-cli, or the
// LINODE_CLI_TOKEN environment variable already set) before this
// module runs. access_token (real linode_v4.py's own required,
// no_log, LINODE_ACCESS_TOKEN-env-fallback argument) IS wired through
// — as LINODE_CLI_TOKEN for that single invocation only, never a
// command-line flag, matching this project's own hard "no secrets in
// argv" rule.
//
// root_pass is ALSO a secret this port must keep off argv. linode-cli
// has no documented environment-variable alternative for it (unlike
// access_token). This port instead passes the bare `--root_pass` flag
// with NO value and pipes the password over stdin — relying on
// Python's own stdlib `getpass()` (linode-cli, like real linode_v4.py
// itself, is a Python CLI) DOCUMENTED fallback behavior: when stdin is
// not a controlling terminal, `getpass()` falls back to reading one
// line from stdin directly rather than refusing to run. This is
// standard, documented Python behavior this port leans on
// deliberately, but it was NOT independently confirmed against a live
// linode-cli invocation in this sandbox (no live Linode account/CLI
// available to test against) — the same class of honestly-flagged,
// bounded risk this batch's own gitlab_common.go doc comment accepts
// for glab's own unverified flag surface. A wrong assumption here
// fails loud (linode-cli's own clean "invalid password"/prompt-timeout
// error), not silently.
//
// # Args
//
// label (required — the sole lookup key, matching real linode_v4.py's
// own `maybe_instance_from_label`); state (required: present|absent);
// access_token (required); region, image, type (required together to
// create, matching real linode_v4.py's own `required_together`);
// authorized_keys (list, repeated --authorized_keys flags);
// private_ip (bool -> bare --private_ip flag); root_pass (secret, see
// above); tags (list, repeated --tags flags); group (-> --group,
// real linode_v4.py's own NOTES call this deprecated-but-supported);
// stackscript_id (-> --stackscript_id); stackscript_data (accepted,
// NOT wired — linode-cli's own flag shape for a StackScript's nested
// UDF data dict was not confirmed, and real linode_v4.py only accepts
// it when stackscript_id is also given, a narrow enough real-world
// case this port judged not worth guessing at without confirmation).
//
// No update path: matching real linode_v4.py's own NOTES ("No Linode
// resizing is currently implemented"), state=present on an
// already-found instance is always a no-op.
//
// Extra["instance"]: the instance's raw JSON object (from `linodes
// view --json` after create, or the matched list entry), present
// whenever the instance now exists.
func moduleLinodeV4(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := linodeRequireBinary(ctx, conn, "linode_v4"); !ok {
		return res, nil
	}
	label, err := requireString(args, "label")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("linode_v4: state must be one of present, absent, got %q", state)
	}
	accessToken, err := requireString(args, "access_token")
	if err != nil {
		return Result{}, err
	}

	var list []map[string]any
	lres, err := linodeCLIJSON(ctx, conn, accessToken, &list, nil, "linodes", "list")
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return linodeFail("linode_v4", "listing instances", lres), nil
	}
	match, found, _ := metalFindByField(list, "label", label)

	if state == "absent" {
		if !found {
			return Ok("linode_v4: "+label+" already absent").WithExtra("instance", map[string]any{}), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := linodeCLI(ctx, conn, accessToken, nil, "linodes", "delete", id)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return linodeFail("linode_v4", "deleting "+label, dres), nil
		}
		return Changed("linode_v4: "+label+" deleted").WithExtra("instance", match), nil
	}

	if found {
		return Ok("linode_v4: "+label+" already present").WithExtra("instance", match), nil
	}

	region := argString(args, "region", "")
	image := argString(args, "image", "")
	linodeType := argString(args, "type", "")
	if region == "" || image == "" || linodeType == "" {
		return Fail("linode_v4: region, image and type are all required together to create " + label), nil
	}

	argv := []string{"linodes", "create", "--label", label, "--region", region, "--image", image, "--type", linodeType}
	for _, k := range argStringList(args, "authorized_keys") {
		argv = append(argv, "--authorized_keys", k)
	}
	for _, t := range argStringList(args, "tags") {
		argv = append(argv, "--tags", t)
	}
	if v := argString(args, "group", ""); v != "" {
		argv = append(argv, "--group", v)
	}
	if argBool(args, "private_ip", false) {
		argv = append(argv, "--private_ip")
	}
	if v := argInt(args, "stackscript_id", 0); v != 0 {
		argv = append(argv, "--stackscript_id", fmt.Sprint(v))
	}
	var stdin *strings.Reader
	rootPass := argString(args, "root_pass", "")
	if rootPass != "" {
		argv = append(argv, "--root_pass")
		stdin = strings.NewReader(rootPass + "\n")
	}

	var created map[string]any
	var cres remoteexec.Result
	if stdin != nil {
		cres, err = linodeCLIJSONStdin(ctx, conn, accessToken, &created, stdin, argv...)
	} else {
		cres, err = linodeCLIJSON(ctx, conn, accessToken, &created, nil, argv...)
	}
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return linodeFail("linode_v4", "creating "+label, cres), nil
	}
	return Changed("linode_v4: "+label+" created").WithExtra("instance", created), nil
}

// linodeRequireBinary fails cleanly (Result{Failed:true}, not a Go
// error) if the real `linode-cli` binary is not on the target's PATH.
func linodeRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v linode-cli"); err != nil {
		return Fail(fmt.Sprintf("%s: the linode-cli binary (Linode's own current official CLI, "+
			"github.com/linode/linode-cli) is required on the target and was not found in PATH — this port "+
			"shells out to it rather than speaking the Linode v4 REST API via linode_api4 directly; see "+
			"linode_v4.go's own doc comment, including the precondition that `linode-cli configure` must "+
			"already have been run (or LINODE_CLI_TOKEN already set) on the target", moduleName)), false
	}
	return Result{}, true
}

// linodeCLI runs one `linode-cli <argv...>` invocation, passing
// authToken (if non-empty) via LINODE_CLI_TOKEN for that single
// command only — never as a command-line flag.
func linodeCLI(ctx context.Context, conn remoteexec.Connection, authToken string, stdin *strings.Reader, argv ...string) (remoteexec.Result, error) {
	full := append([]string{"linode-cli"}, argv...)
	quoted := make([]string, len(full))
	for i, a := range full {
		quoted[i] = shellQuote(a)
	}
	cmd := strings.Join(quoted, " ")
	if authToken != "" {
		cmd = "LINODE_CLI_TOKEN=" + shellQuote(authToken) + " " + cmd
	}
	if stdin != nil {
		return conn.Exec(ctx, cmd, stdin)
	}
	return conn.Exec(ctx, cmd, nil)
}

// linodeCLIJSON is linodeCLI plus `--json`, decoding stdout into out
// on success.
func linodeCLIJSON(ctx context.Context, conn remoteexec.Connection, authToken string, out any, stdin *strings.Reader, argv ...string) (remoteexec.Result, error) {
	res, err := linodeCLI(ctx, conn, authToken, stdin, append(argv, "--json")...)
	if err != nil {
		return res, err
	}
	if res.RC != 0 || out == nil || strings.TrimSpace(res.Stdout) == "" {
		return res, nil
	}
	if jerr := json.Unmarshal([]byte(res.Stdout), out); jerr != nil {
		return res, fmt.Errorf("decoding linode-cli %s output: %w", strings.Join(argv, " "), jerr)
	}
	return res, nil
}

// linodeCLIJSONStdin is linodeCLIJSON with a required (non-nil) stdin
// — a small readability wrapper for call sites that always pipe a
// secret over stdin (see moduleLinodeV4's own doc comment on
// root_pass).
func linodeCLIJSONStdin(ctx context.Context, conn remoteexec.Connection, authToken string, out any, stdin *strings.Reader, argv ...string) (remoteexec.Result, error) {
	return linodeCLIJSON(ctx, conn, authToken, out, stdin, argv...)
}

// linodeFail builds a Fail() message from a non-zero linode-cli
// result, preferring stderr but falling back to stdout.
func linodeFail(moduleName, action string, res remoteexec.Result) Result {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return Fail(fmt.Sprintf("%s: %s: %s", moduleName, action, msg))
}
