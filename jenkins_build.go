package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJenkinsBuild implements Ansible's `jenkins_build`
// (community.general) module: triggers (and optionally waits for) a
// Jenkins build — see jenkins_common.go's own doc comment for the
// jenkins-cli.jar substitution shared by this batch's other REST/CLI-
// facing jenkins_* modules. Command used for state=present: `build
// <job> [-p key=value ...] [-s] [-v]` — jenkins-cli's own documented
// "Starts a build, and optionally waits for completion" command
// (confirmed via this batch's own research), whose own `-s`
// ("wait until the completion/abortion of this build") is the natural
// fit for this module's own `detach=false` (the default: wait for the
// build to finish before returning).
//
// Args: name (required); args (dict of build parameters, rendered as
// repeated -p key=value); detach (bool, default false — when false,
// `-s` is added so this port's own call blocks until the build
// finishes, matching real jenkins_build.py's own default polling
// behavior via time_between_checks); user, password, token; url;
// state (present|absent|stopped, default present).
//
// Deviation — state=absent/stopped: real jenkins_build.py's
// state=absent cancels a build still sitting in Jenkins' BUILD QUEUE
// (an item that hasn't started running yet), and state=stopped aborts
// an already-RUNNING build — both via python-jenkins' own
// queue-cancel-item/stop-build REST calls. jenkins-cli has no
// subcommand for either operation (confirmed: neither queue
// cancellation nor build abortion appears in Jenkins' own documented
// built-in CLI command set) — this port therefore Fails loud
// (Result{Failed:true}) for state=absent/stopped, per this batch's own
// instructions to fail loud rather than silently fake parity when a
// real capability genuinely has no CLI equivalent, rather than
// pretending success or silently treating it as a no-op.
//
// state=present with detach=true does not, and cannot, report the
// triggered build's own eventual result (matching real
// jenkins_build.py's own documented behavior in that mode); with
// detach=false (the default), `build -s`'s own non-zero exit on a
// failed/aborted build is reported as a Fail with jenkins-cli's own
// error text (the closest this port can get to real jenkins_build.py's
// own `result`/`build_info` fields without a REST call it has no
// client library to make).
func moduleJenkinsBuild(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := jenkinsRequireRuntime(ctx, conn, "jenkins_build"); !ok {
		return res, nil
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	url := argString(args, "url", "http://localhost:8080")
	user, password, token := jenkinsAuthArgs(args, "user")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "stopped" {
		return Result{}, errArg("jenkins_build: state must be one of present, absent, stopped, got %q", state)
	}
	if state != "present" {
		return Fail(fmt.Sprintf("jenkins_build: state=%s is not supported by this port — jenkins-cli has no "+
			"queue-cancel or build-abort command (real jenkins_build.py does this via python-jenkins' own "+
			"queue/stop-build REST calls, which this port has no client for); see jenkins_build.go's own doc "+
			"comment", state)), nil
	}

	argv := []string{"build", name}
	if params, ok := args["args"].(map[string]any); ok {
		for k, v := range params {
			argv = append(argv, "-p", k+"="+fmt.Sprint(v))
		}
	}
	if !argBool(args, "detach", false) {
		argv = append(argv, "-s")
	}

	res, err := jenkinsRun(ctx, conn, url, user, password, token, nil, argv...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("jenkins_build: build of " + name + " failed: " + jenkinsErrMsg(res)), nil
	}
	return Changed("jenkins_build: "+name+" build triggered").WithExtra("name", name).WithExtra("state", "present"), nil
}
