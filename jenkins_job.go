package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJenkinsJob implements Ansible's `jenkins_job`
// (community.general) module: creates, updates, enables/disables, or
// deletes a Jenkins job — see jenkins_common.go's own doc comment for
// the jenkins-cli.jar substitution shared by this batch's other
// REST/CLI-facing jenkins_* modules. Commands used: `get-job`/
// `create-job`/`update-job`/`delete-job`/`enable-job`/`disable-job` —
// all long-standing, extremely stable jenkins-cli built-ins present
// since Jenkins' CLI was first introduced (high confidence, unlike
// several of this sub-batch's more obscure commands whose doc
// comments flag lower confidence explicitly).
//
// Args: name (required); config (XML string — required if the job
// does not yet exist; mutually exclusive with enabled); enabled (bool,
// mutually exclusive with config); user, password, token (auth — see
// jenkins_common.go's own doc comment); url (default
// http://localhost:8080); validate_certs (accepted for argument-shape
// compatibility, but NOT wired — a documented gap: honoring it would
// need both curl's own -k for this port's own jar-fetch precondition
// AND Java's own JVM-wide TLS trust-store flags for jenkins-cli's
// subsequent HTTPS calls, judged not worth the added complexity for
// what real jenkins_job.py itself only uses as a python-jenkins/
// requests verify=False passthrough); state (present|absent, default
// present).
//
// present: get-job first (RC!=0/jenkinsNotFound -> not present). Not
// present -> create-job <name> with config piped over stdin (Fail with
// a clear message if config was not given — matching real
// jenkins_job.py's own required-if constraint). Present -> if config
// was given and differs from the current XML (get-job's own stdout),
// update-job <name> with the new config piped over stdin; if enabled
// was given, enable-job/disable-job as needed (get-job's own XML is
// grepped for a top-level `<disabled>true</disabled>` to determine
// current enabled state — jenkins-cli has no separate "is this job
// enabled" query). Neither config nor enabled given and the job
// already exists is a no-op.
//
// absent: delete-job <name> if present, else a no-op.
func moduleJenkinsJob(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := jenkinsRequireRuntime(ctx, conn, "jenkins_job"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	url := argString(args, "url", "http://localhost:8080")
	user, password, token := jenkinsAuthArgs(args, "user")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("jenkins_job: state must be one of present, absent, got %q", state)
	}
	_, hasConfig := args["config"]
	configXML := argString(args, "config", "")
	_, enabledGiven := args["enabled"]
	enabled := argBool(args, "enabled", true)
	if hasConfig && enabledGiven {
		return Result{}, errArg("jenkins_job: config and enabled are mutually exclusive")
	}

	getRes, err := jenkinsRun(ctx, conn, url, user, password, token, nil, "get-job", name)
	if err != nil {
		return Result{}, err
	}
	found := getRes.RC == 0
	if !found && !jenkinsNotFound(getRes) {
		return Fail("jenkins_job: unable to check job " + name + ": " + jenkinsErrMsg(getRes)), nil
	}

	if state == "absent" {
		if !found {
			return Ok("jenkins_job: " + name + " already absent"), nil
		}
		dres, err := jenkinsRun(ctx, conn, url, user, password, token, nil, "delete-job", name)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("jenkins_job: unable to delete " + name + ": " + jenkinsErrMsg(dres)), nil
		}
		return Changed("jenkins_job: " + name + " deleted"), nil
	}

	if !found {
		if configXML == "" {
			return Fail("jenkins_job: config is required to create job " + name + " (it does not yet exist)"), nil
		}
		cres, err := jenkinsRun(ctx, conn, url, user, password, token, strings.NewReader(configXML), "create-job", name)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("jenkins_job: unable to create " + name + ": " + jenkinsErrMsg(cres)), nil
		}
		return Changed("jenkins_job: " + name + " created"), nil
	}

	changed := false
	if hasConfig && configXML != "" && strings.TrimSpace(configXML) != strings.TrimSpace(getRes.Stdout) {
		ures, err := jenkinsRun(ctx, conn, url, user, password, token, strings.NewReader(configXML), "update-job", name)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return Fail("jenkins_job: unable to update " + name + ": " + jenkinsErrMsg(ures)), nil
		}
		changed = true
	}
	if enabledGiven {
		currentlyDisabled := strings.Contains(getRes.Stdout, "<disabled>true</disabled>")
		if enabled == currentlyDisabled {
			cmd := "enable-job"
			if !enabled {
				cmd = "disable-job"
			}
			eres, err := jenkinsRun(ctx, conn, url, user, password, token, nil, cmd, name)
			if err != nil {
				return Result{}, err
			}
			if eres.RC != 0 {
				return Fail("jenkins_job: unable to " + cmd + " " + name + ": " + jenkinsErrMsg(eres)), nil
			}
			changed = true
		}
	}
	if !changed {
		return Ok("jenkins_job: " + name + " already up to date"), nil
	}
	return Changed("jenkins_job: " + name + " updated"), nil
}
