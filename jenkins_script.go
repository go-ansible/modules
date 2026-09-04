package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJenkinsScript implements Ansible's `jenkins_script`
// (community.general) module: runs an arbitrary Groovy script against
// a Jenkins controller's own Script Console — see jenkins_common.go's
// own doc comment for the jenkins-cli.jar substitution shared by this
// batch's other REST/CLI-facing jenkins_* modules. Command used:
// `groovy =` (reads the script from stdin — jenkins-cli's own
// documented form for running a script that isn't already a file on
// the CONTROL node's filesystem, which is exactly this module's own
// shape: `script` is a string argument, not a path), the single most
// direct, purpose-built jenkins-cli command for what this module does
// — arguably a closer architectural match than any other jenkins_*
// module in this batch, since real jenkins_script.py's own POST to
// Jenkins' `/scriptText` endpoint and jenkins-cli's `groovy =` both
// ultimately run the same server-side Groovy script console.
//
// Args: script (required); args (dict — used to render `script` as a
// Python string.Template before sending, matching real
// jenkins_script.py's own exact templating mechanism: `$var`/`${var}`
// substitution, not Go's text/template syntax); user, password, token
// (auth); url (default a local, unproxied Jenkins:
// http://localhost:8080); timeout (accepted, inert — this port's own
// Connection.Exec has no per-call timeout knob independent of ctx, and
// ctx's own deadline/cancellation, set by this port's caller, already
// governs how long any single module invocation may run); validate_certs
// (accepted, not wired — see jenkins_job.go's own doc comment for
// why).
//
// Templating: `$var`/`${var}` tokens in script are replaced with
// args's own same-named values (as fmt.Sprint), mirroring Python's
// string.Template substitution rule closely enough for this port's own
// purposes — an UNESCAPED `$$` (Python's own literal-dollar escape) is
// NOT specially handled, a documented, narrow gap for the one Python
// string.Template feature this port's own simple replacer does not
// reproduce.
//
// No changed-detection: matching real jenkins_script.py's own NOTES
// ("Since the script can do anything this does not report on changes.
// ... it is important to set `changed_when`"), this port always
// returns Changed=true on a successful run (RC==0) — the caller's own
// task is expected to set changed_when itself.
//
// Extra["output"]: the script's own stdout (groovy's own `println`
// output, matching real jenkins_script.py's own `output` return
// value).
func moduleJenkinsScript(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := jenkinsRequireRuntime(ctx, conn, "jenkins_script"); !ok {
		return res, nil
	}
	script, err := requireString(args, "script")
	if err != nil {
		return Result{}, err
	}
	url := argString(args, "url", "http://localhost:8080")
	user, password, token := jenkinsAuthArgs(args, "user")

	if tmplArgs, ok := args["args"].(map[string]any); ok {
		for k, v := range tmplArgs {
			val := fmt.Sprint(v)
			script = strings.ReplaceAll(script, "${"+k+"}", val)
			script = strings.ReplaceAll(script, "$"+k, val)
		}
	}

	res, err := jenkinsRun(ctx, conn, url, user, password, token, strings.NewReader(script), "groovy", "=")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("jenkins_script: script failed: " + jenkinsErrMsg(res)), nil
	}
	return Changed("jenkins_script: script executed").WithExtra("output", res.Stdout), nil
}
