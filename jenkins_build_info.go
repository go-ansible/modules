package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJenkinsBuildInfo implements Ansible's `jenkins_build_info`
// (community.general) module: a read-only query for one Jenkins
// build's own result/status — see jenkins_common.go's own doc comment
// for the jenkins-cli.jar substitution shared by this batch's other
// REST/CLI-facing jenkins_* modules.
//
// Deviation — no direct CLI command: jenkins-cli's own `console`
// command returns unstructured build LOG TEXT, not the structured
// result/building/timestamp/duration fields real jenkins_build_info.py
// reads from python-jenkins' `get_build_info()` (a REST JSON call).
// jenkins-cli has no command that returns that same structured data.
// This port bridges the gap via `groovy =` (see jenkins_script.go's
// own doc comment for that command) — the SAME command
// jenkins_script.go itself wraps for arbitrary script execution — with
// a small, fixed Groovy script this port writes and pipes over stdin,
// which fetches the job/build objects through Jenkins' own internal
// Java API (Jenkins.instance.getItemByFullName, run.getResult(), ...)
// and prints them back as one line of JSON via groovy.json.JsonOutput.
// This is a genuine, deliberate architectural choice (not a silent
// guess): it reaches real, live Jenkins state that no built-in
// jenkins-cli command exposes, using a mechanism (Groovy script
// execution against the controller) real Jenkins administrators
// themselves reach for whenever the CLI's own fixed command set falls
// short — but it does mean this module's own correctness rests on
// this port's own Groovy script being right, not on a documented CLI
// command's own stable contract; a script error surfaces as a clean
// Fail with Jenkins' own Groovy stack trace, not a silent wrong
// answer.
//
// Args: name (required); build_number (optional — last build when
// omitted, matching real jenkins_build_info.py's own documented
// default); user, password, token; url; validate_certs (accepted, not
// wired — see jenkins_job.go's own doc comment).
//
// Extra["build_info"]: {"result":..., "building":..., "number":...,
// "timestamp":..., "duration":..., "url":...} — a subset of real
// jenkins_build_info.py's own `build_info` return value (the fields
// this port's own fixed Groovy script reads), or Fail
// (Result{Failed:true}) if the job or build does not exist.
func moduleJenkinsBuildInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := jenkinsRequireRuntime(ctx, conn, "jenkins_build_info"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	url := argString(args, "url", "http://localhost:8080")
	user, password, token := jenkinsAuthArgs(args, "user")
	buildNumber := argInt(args, "build_number", 0)

	script := jenkinsBuildInfoScript(name, buildNumber)
	res, err := jenkinsRun(ctx, conn, url, user, password, token, strings.NewReader(script), "groovy", "=")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("jenkins_build_info: unable to query " + name + ": " + jenkinsErrMsg(res)), nil
	}
	line := strings.TrimSpace(res.Stdout)
	if line == "NOJOB" {
		return Fail("jenkins_build_info: no such job " + name), nil
	}
	if line == "NOBUILD" {
		return Fail("jenkins_build_info: job " + name + " has no matching build"), nil
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(line), &info); err != nil {
		return Fail("jenkins_build_info: could not parse groovy script output: " + err.Error()), nil
	}
	return Ok("jenkins_build_info: "+name).
		WithExtra("build_info", info).WithExtra("name", name).WithExtra("state", "present"), nil
}

// jenkinsBuildInfoScript renders the fixed Groovy script
// moduleJenkinsBuildInfo pipes to `groovy =` — see that function's own
// doc comment for why this exists instead of a direct jenkins-cli
// command. jobName is embedded as a Groovy single-quoted string
// literal (its own quotes/backslashes escaped); buildNumber<=0 means
// "the last build" (run = job.lastBuild), matching real
// jenkins_build_info.py's own documented default.
func jenkinsBuildInfoScript(jobName string, buildNumber int) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(jobName)
	buildExpr := "job.lastBuild"
	if buildNumber > 0 {
		buildExpr = fmt.Sprintf("job.getBuildByNumber(%d)", buildNumber)
	}
	return fmt.Sprintf(`
import groovy.json.JsonOutput
def job = Jenkins.instance.getItemByFullName('%s')
if (job == null) { println 'NOJOB'; return }
def run = %s
if (run == null) { println 'NOBUILD'; return }
def m = [
  result: run.result?.toString(),
  building: run.isBuilding(),
  number: run.number,
  timestamp: run.timeInMillis,
  duration: run.duration,
  url: run.absoluteUrl,
]
println JsonOutput.toJson(m)
`, escaped, buildExpr)
}
