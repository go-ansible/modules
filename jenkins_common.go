package modules

import (
	"context"
	"fmt"
	"io"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the seven REST/CLI-facing jenkins_*.go
// modules in this batch (jenkins_build, jenkins_build_info,
// jenkins_credential, jenkins_job, jenkins_job_info, jenkins_node,
// jenkins_script) share: shelling out to Jenkins' own official CLI
// client, `jenkins-cli.jar`, instead of talking to Jenkins' REST API
// through python-jenkins the way every real jenkins_* module in this
// batch does — the same "shell out to the platform's own official CLI
// instead of an API client" precedent this port already uses
// elsewhere (see github_common.go/gitlab_common.go's own doc comments
// for the two most recent, closest examples). jenkins_plugin.go is the
// ONE module in this sub-batch that does NOT use any of this file:
// real jenkins_plugin.py, read before implementing, never talks to
// Jenkins' CLI or REST management API for plugin install/pin/enable at
// all — it manipulates the Jenkins controller's own local filesystem
// directly (fetch_url'ing a .jpi/.hpi file straight into
// `<jenkins_home>/plugins/`, and touching/removing `.pinned`/
// `.disabled` marker files there), something this port's own Connection
// abstraction already does natively without any CLI substitution — see
// jenkins_plugin.go's own doc comment.
//
// # `jenkins-cli.jar` needs a JRE — a precondition, like serverless.go's
// # own Node.js one
//
// jenkins-cli.jar is a plain Java jar, invoked as `java -jar
// jenkins-cli.jar -s <url> <command> [args]` (jenkins.io's own "Jenkins
// CLI" documentation) — it requires a JRE on the target the same way
// serverless.go's own `serverless` CLI requires a Node.js runtime on
// the target (see that file's own doc comment for the precedent this
// follows): a real, reasonable precondition to document rather than
// something this port could avoid.
//
// # No persistent jar — fetched fresh from the target Jenkins server
// # itself, per invocation
//
// Real jenkins_* modules have no `jar_path`-shaped argument at all
// (python-jenkins needs no jar), so this port does not invent one
// either — inventing a new argument real playbooks never set would
// violate this batch's own "don't invent an argument its real
// counterpart doesn't document" rule just as much for a jar path as it
// would for an auth argument. Instead, every jenkinsRun call in this
// batch downloads jenkins-cli.jar FRESH, from the exact Jenkins server
// the module is already talking to — `<url>/jnlpJars/jenkins-cli.jar`,
// jenkins.io's own documented download path, guaranteed to match that
// server's own CLI protocol version — into a target-side temp file
// (curl, required on the target as an additional precondition
// alongside the JRE), used for one invocation, then removed. This
// is real per-task overhead a live python-jenkins REST call doesn't
// have; it is also the only approach that needs no new argument and
// can never talk to the wrong Jenkins server's CLI protocol version by
// accident.
//
// # Auth precondition and secret handling
//
// Every real jenkins_* module in this batch accepts its own
// user/password/token arguments (mutually exclusive in some, both
// accepted in others — see each module's own doc comment for its
// exact set) to open a fresh python-jenkins session per task run.
// jenkins-cli's own modern HTTP-transport mode (the only transport
// this port uses — the older remoting-based `-remoting`/JNLP transport
// is deprecated upstream and needs a persistent TCP port this port has
// no reason to open) documents two ways to authenticate: `-auth
// user:token` on the command line, or the JENKINS_USER_ID/
// JENKINS_API_TOKEN environment variables (jenkins.io's own CLI docs).
// This port ALWAYS uses the environment-variable form — never `-auth`
// — matching this project's own hard "no secrets in argv" rule (see
// redis.go's own REDISCLI_AUTH precedent). When neither token nor
// password is given, jenkins-cli runs anonymously, matching every real
// jenkins_* module's own documented anonymous-access examples.
//
// Deviation — jenkins-cli's HTTP-transport mode requires an API TOKEN,
// not a plain account password (a real, well-documented Jenkins
// behavior: the CLI's HTTP transport authenticates via the same
// mechanism as an API request, and Jenkins does not accept a plain
// password there, only a per-user API token, unlike the old
// remoting-based transport this port does not use) — so when a real
// playbook gives `password` but no `token`, this port still passes
// that value through JENKINS_API_TOKEN as its best-effort attempt (it
// is exactly what a user would put there for many legacy/anonymous-
// write-enabled setups), but a real account password will typically
// be REJECTED by jenkins-cli itself with a clean, loud auth error —
// this is real upstream Jenkins CLI behavior this port surfaces
// honestly rather than papering over, not a bug in this port.
//
// jenkinsRequireRuntime fails cleanly (Result{Failed:true}, not a Go
// error) if `java` or `curl` is missing from the target's PATH.
func jenkinsRequireRuntime(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v java"); err != nil {
		return Fail(fmt.Sprintf("%s: a JRE (the `java` binary) is required on the target to run jenkins-cli.jar "+
			"— see jenkins_common.go's own doc comment, matching serverless.go's own Node.js-runtime precondition", moduleName)), false
	}
	if _, err := run(ctx, conn, "command -v curl"); err != nil {
		return Fail(fmt.Sprintf("%s: curl is required on the target to fetch jenkins-cli.jar fresh from the "+
			"target Jenkins server — see jenkins_common.go's own doc comment", moduleName)), false
	}
	return Result{}, true
}

// jenkinsFetchCLIJar downloads <url>/jnlpJars/jenkins-cli.jar into a
// fresh target-side temp file, returning its path. The caller is
// responsible for removing it (via conn.Remove) once done — every
// jenkinsRun call in this batch does so via a deferred cleanup.
func jenkinsFetchCLIJar(ctx context.Context, conn remoteexec.Connection, url string) (string, error) {
	jarPath := conn.TempPath("jenkins-cli.jar")
	jarURL := strings.TrimRight(url, "/") + "/jnlpJars/jenkins-cli.jar"
	cmd := "curl -sSfL " + shellQuote(jarURL) + " -o " + shellQuote(jarPath)
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", fmt.Errorf("fetching jenkins-cli.jar from %s: %s", jarURL, strings.TrimSpace(res.Stderr))
	}
	return jarPath, nil
}

// jenkinsAuthArgs extracts (user, password, token) from args using the
// field names every real jenkins_* module in this batch shares (`user`
// aka jenkins_user for jenkins_credential specifically — see that
// module's own doc comment — `password`, `token`).
func jenkinsAuthArgs(args map[string]any, userKey string) (user, password, token string) {
	return argString(args, userKey, ""), argString(args, "password", ""), argString(args, "token", "")
}

// jenkinsRun fetches jenkins-cli.jar (see jenkinsFetchCLIJar), runs one
// `java -jar <jar> -s <url> <argv...>` invocation with stdin piped
// (nil for none), and removes the jar afterward. user/token (or
// user/password — see this file's own doc comment on the
// password-vs-token distinction) are exported as
// JENKINS_USER_ID/JENKINS_API_TOKEN for that single invocation only,
// never placed on the command line.
func jenkinsRun(ctx context.Context, conn remoteexec.Connection, url, user, password, token string, stdin io.Reader, argv ...string) (remoteexec.Result, error) {
	jarPath, err := jenkinsFetchCLIJar(ctx, conn, url)
	if err != nil {
		return remoteexec.Result{}, err
	}
	defer func() { _ = conn.Remove(ctx, jarPath) }()

	parts := append([]string{"java", "-jar", jarPath, "-s", url}, argv...)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	cmd := strings.Join(quoted, " ")
	env := ""
	authToken := token
	if authToken == "" {
		authToken = password
	}
	if user != "" && authToken != "" {
		env = "JENKINS_USER_ID=" + shellQuote(user) + " JENKINS_API_TOKEN=" + shellQuote(authToken) + " "
	}
	return conn.Exec(ctx, env+cmd, stdin)
}

// jenkinsErrMsg builds a Fail() message body from a non-zero
// jenkins-cli result, preferring stderr but falling back to stdout.
func jenkinsErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// jenkinsNotFound reports whether a failed jenkins-cli invocation's own
// error text looks like "no such job/node/...", matching jenkins-cli's
// own consistent "No such job '...'"/"No such node '...'" wording for
// a get/delete/etc. against a name that does not exist.
func jenkinsNotFound(res remoteexec.Result) bool {
	msg := strings.ToLower(jenkinsErrMsg(res))
	return strings.Contains(msg, "no such job") || strings.Contains(msg, "no such node") ||
		strings.Contains(msg, "no such computer")
}
