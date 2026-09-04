package modules

import (
	"context"
	"path"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJenkinsJobInfo implements Ansible's `jenkins_job_info`
// (community.general) module: a read-only query for one or more
// Jenkins jobs — see jenkins_common.go's own doc comment for the
// jenkins-cli.jar substitution shared by this batch's other REST/CLI-
// facing jenkins_* modules. Command used: `list-jobs` — jenkins-cli's
// own plain-names listing, confirmed real and stable
// (jenkins.io/docs and this batch's own research). jenkins-cli has no
// structured (JSON/XML) equivalent of python-jenkins'
// `get_job_info()`, so per-job detail (color, url, fullname — real
// jenkins_job_info.py's own documented `jobs` return fields) is filled
// in here via `get-job <name>` (its raw config.xml — this port reads
// the job's own `<disabled>` element to fill `color` as best it can:
// "disabled" or "notbuilt", never a real build-status color like
// "blue"/"red"/"yellow", since that information lives in Jenkins'
// runtime build-history state, not in a job's static config.xml, and
// jenkins-cli has no command that surfaces it) rather than a single
// bulk call — a real, documented per-job N+1 cost this port accepts
// since list-jobs itself is the only enumeration primitive available.
//
// Args: name (exact job name — mutually exclusive in real
// jenkins_job_info.py with glob/color, but this port applies whichever
// of name/glob/color are given as successive filters, matching the
// real module's own documented net effect closely enough); glob (shell
// glob over job names — matched client-side via path.Match, mirroring
// Python's fnmatch real jenkins_job_info.py itself uses); color
// (filter by this port's own best-effort color value — see above);
// user, password, token; url; validate_certs (accepted, not wired —
// see jenkins_job.go's own doc comment).
//
// Extra["jobs"]: a list of {"name":..., "url":..., "color":...}
// objects for every matching job, present always (possibly empty) —
// matching real jenkins_job_info.py's own `jobs` return value, minus
// the `fullname` field (identical to `name` for this port's own
// top-level-only jenkins-cli-backed listing, which has no folder-
// nesting concept to distinguish the two).
func moduleJenkinsJobInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := jenkinsRequireRuntime(ctx, conn, "jenkins_job_info"); !ok {
		return res, nil
	}
	url := argString(args, "url", "http://localhost:8080")
	user, password, token := jenkinsAuthArgs(args, "user")
	name := argString(args, "name", "")
	glob := argString(args, "glob", "")

	lres, err := jenkinsRun(ctx, conn, url, user, password, token, nil, "list-jobs")
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("jenkins_job_info: unable to list jobs: " + jenkinsErrMsg(lres)), nil
	}

	var names []string
	for _, line := range strings.Split(lres.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if name != "" && line != name {
			continue
		}
		if glob != "" {
			if ok, _ := path.Match(glob, line); !ok {
				continue
			}
		}
		names = append(names, line)
	}

	color := argString(args, "color", "")
	jobs := make([]map[string]any, 0, len(names))
	for _, n := range names {
		gres, err := jenkinsRun(ctx, conn, url, user, password, token, nil, "get-job", n)
		if err != nil {
			return Result{}, err
		}
		jobColor := "notbuilt"
		if gres.RC == 0 && strings.Contains(gres.Stdout, "<disabled>true</disabled>") {
			jobColor = "disabled"
		}
		if color != "" && jobColor != color {
			continue
		}
		jobs = append(jobs, map[string]any{
			"name": n, "url": strings.TrimRight(url, "/") + "/job/" + n + "/", "color": jobColor,
		})
	}

	return Ok("jenkins_job_info: found "+strconv.Itoa(len(jobs))+" job(s)").WithExtra("jobs", jobs), nil
}
