package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what every gitlab_* module in this batch shares:
// shelling out to `glab` (GitLab's own official CLI) instead of
// python-gitlab (a Python REST API client), the same "shell out to the
// platform's own official CLI instead of an API client" substitution
// this port already makes for Consul (consul_kv.go/consul_session.go),
// Redis (redis.go), Terraform (terraform.go), Icinga2, and Kopia — and,
// in this exact batch, for GitHub's gh_* modules via the `gh` CLI (see
// that batch's own github_common.go, written by a sibling agent; not a
// file this one touches).
//
// # Auth precondition
//
// Real gitlab_* modules authenticate per-invocation via their own
// api_url/api_token/api_username/api_password/api_oauth_token/
// api_job_token arguments, opening a fresh python-gitlab session each
// task run. `glab` has no equivalent per-invocation login flow: it
// always talks to whatever GitLab host/token is already configured in
// its own persistent config (via a prior `glab auth login`) or supplied
// through the GITLAB_TOKEN/GL_TOKEN environment variables already set
// on the target — exactly the same shape of narrowing ipa_common.go's
// own doc comment documents for `ipa` and a prior `kinit`: this port
// does not attempt to manage `glab` authentication itself. So, for
// every gitlab_* module in this batch:
//   - api_token/api_oauth_token/api_job_token/api_username/api_password/
//     api_url/ca_path/validate_certs are all accepted (for argument-shape
//     compatibility with real playbooks written against real gitlab_*
//     modules) but have NO EFFECT on this port's behavior — they are not
//     wired into the `glab` invocation in any way. This is a deliberate,
//     honestly-documented gap, matching ipa_common.go's own stance
//     exactly, not a silent misinterpretation.
//   - `glab` must already be authenticated on the target (a prior `glab
//     auth login`, or GITLAB_TOKEN/GL_TOKEN already exported in the
//     invoking shell's environment) before this port's gitlab_* modules
//     run. This port does not manage that authentication itself.
//
// # `glab api` as the generic fallback
//
// `glab` has dedicated subcommands for some resources (`glab repo
// create`/`view`/`delete`) but not for most of what this batch's ten
// modules manage — there is no dedicated `glab` subcommand for project
// milestones, access tokens, approval rules, badges, membership,
// CI/CD variables, protected branches, runner registration, or user
// administration. For all of those, every module in this batch falls
// back to `glab api <path>` — glab's own generic GitLab-API-passthrough
// subcommand (the same role `gh api` plays for gh_*.go's own modules in
// this batch), documented per-module in each file's own doc comment for
// exactly which resource path it drives. `glab` was not available to
// install and exercise directly in this port's own sandbox (see this
// comment's own note below); `glab api`'s flag surface here (-X for the
// HTTP method, --input - to send a JSON body over stdin, --paginate to
// follow a paginated list to completion) is applied on the strength of
// glab's own published command reference, which documents `api` as
// deliberately gh-api-compatible, not verified against a live glab
// binary in this sandbox — the same honesty this port's whole "read the
// reference before implementing" rule asks for when a live check truly
// isn't possible.
//
// # Project/group identifiers
//
// A GitLab API `:id` path parameter accepts either the resource's
// numeric ID or its URL-encoded full path (`group/subgroup/project`) —
// documented explicitly by GitLab's own API reference — so every module
// in this batch passes `project`/`group` straight through
// glabEncodeID, never resolving a path to a numeric ID first, EXCEPT
// where the underlying GitLab endpoint's own request body has no path
// form at all (project creation's `namespace_id`, which the projects
// endpoint only accepts as a plain integer) — those call sites use
// glabResolveGroupID instead, documented at each call site.
//
// # Secrets
//
// No gitlab_* module in this batch ever places a token on the command
// line or in an environment variable this port sets itself — matching
// this project's own hard "no secrets in argv" rule (see redis.go's own
// REDISCLI_AUTH doc comment for the established convention) — because
// `glab` needs none from this port at all: its own already-configured
// auth (see above) is what every invocation uses.
// glabRequireBinary fails cleanly (Result{Failed:true}, not a Go error)
// if the real `glab` CLI is not on the target's PATH.
func glabRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v glab"); err != nil {
		return Fail(fmt.Sprintf("%s: the glab binary (GitLab's own CLI) is required on the target and was "+
			"not found in PATH — this port shells out to it rather than speaking the GitLab REST API via "+
			"python-gitlab directly; see gitlab_common.go's own doc comment, including the precondition "+
			"that `glab auth login` must already have been run (or GITLAB_TOKEN/GL_TOKEN already set) on "+
			"the target", moduleName)), false
	}
	return Result{}, true
}

// glabResult is the outcome of one `glab api` invocation.
type glabResult struct {
	RC     int
	Stdout string
	Stderr string
}

// glabEncodeID URL-path-encodes a GitLab resource identifier (a numeric
// ID passes through unchanged; a "group/subgroup/project"-shaped path
// has its "/" separators percent-encoded to %2F, matching what GitLab's
// own API reference documents its `:id` path parameter expects for a
// path-form identifier).
func glabEncodeID(id string) string {
	return url.PathEscape(id)
}

// glabAPI runs `glab api <path> -X <method>` (plus --paginate when
// requested, plus --input - piping body over stdin when body is
// non-nil) and returns its raw result — RC not treated as an error,
// callers decide what a non-zero exit means (glabIsNotFound for a
// probe, glabErrMsg for a real failure).
func glabAPI(ctx context.Context, conn remoteexec.Connection, method, path string, body []byte, paginate bool) (glabResult, error) {
	parts := []string{"glab", "api", path, "-X", method}
	if paginate {
		parts = append(parts, "--paginate")
	}
	if body != nil {
		parts = append(parts, "--input", "-")
	}
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	cmd := strings.Join(quoted, " ")
	var stdin *strings.Reader
	if body != nil {
		stdin = strings.NewReader(string(body))
	}
	var res remoteexec.Result
	var err error
	if stdin != nil {
		res, err = conn.Exec(ctx, cmd, stdin)
	} else {
		res, err = conn.Exec(ctx, cmd, nil)
	}
	if err != nil {
		return glabResult{}, err
	}
	return glabResult{RC: res.RC, Stdout: res.Stdout, Stderr: res.Stderr}, nil
}

// glabAPIJSON is glabAPI plus JSON marshal-in/unmarshal-out: body (if
// non-nil) is JSON-encoded for the request; on a successful (RC==0)
// response, out (if non-nil) is JSON-decoded from stdout. found reports
// whether the resource existed (RC==0, or RC!=0 without a 404 — that
// second case is a real failure the caller must still check
// separately: found only ever means "not a 404", it is not itself a
// success flag).
func glabAPIJSON(ctx context.Context, conn remoteexec.Connection, method, path string, body any, paginate bool, out any) (res glabResult, err error) {
	var b []byte
	if body != nil {
		b, err = json.Marshal(body)
		if err != nil {
			return glabResult{}, fmt.Errorf("encoding request body: %w", err)
		}
	}
	res, err = glabAPI(ctx, conn, method, path, b, paginate)
	if err != nil {
		return glabResult{}, err
	}
	if res.RC == 0 && out != nil && strings.TrimSpace(res.Stdout) != "" {
		if uerr := json.Unmarshal([]byte(res.Stdout), out); uerr != nil {
			return res, fmt.Errorf("decoding response from %s %s: %w", method, path, uerr)
		}
	}
	return res, nil
}

// glabIsNotFound reports whether a non-zero glab api result looks like
// a 404 — `glab api` (mirroring `gh api`) reports the HTTP status in
// its own error text on stderr (falling back to stdout, since some
// glab versions print it there instead) as "... (HTTP 404)" or
// "404 Not Found" or similar; this port greps for the literal "404"
// digit sequence rather than a stricter pattern, since it has no live
// glab binary in this sandbox to pin the exact wording against (see
// gitlab_common.go's own doc comment).
func glabIsNotFound(res glabResult) bool {
	if res.RC == 0 {
		return false
	}
	return strings.Contains(res.Stderr, "404") || strings.Contains(res.Stdout, "404")
}

// glabErrMsg builds a Fail() message from a non-zero glab api result,
// preferring stderr but falling back to stdout.
func glabErrMsg(res glabResult) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// glabAccessLevels maps every access-level spelling this batch's
// gitlab_* modules' own real doc pages document (across
// gitlab_project_access_token, gitlab_project_members, gitlab_user,
// gitlab_protected_branch, gitlab_runner) to the numeric GitLab API
// access level it corresponds to — verified against GitLab's own API
// documentation's permissions table, not guessed: no one/nobody=0,
// guest=10, planner=15, reporter=20, developer=30, maintainer=40
// (master is real GitLab's own deprecated pre-11.0 alias for
// maintainer, still accepted by several of this batch's real modules'
// own `choices`), owner=50.
var glabAccessLevels = map[string]int{
	"no one":     0,
	"nobody":     0,
	"guest":      10,
	"planner":    15,
	"reporter":   20,
	"developer":  30,
	"maintainer": 40,
	"master":     40,
	"owner":      50,
}

// glabAccessLevel resolves a GitLab access-level name to its numeric
// API value.
func glabAccessLevel(name string) (int, error) {
	if n, ok := glabAccessLevels[strings.ToLower(name)]; ok {
		return n, nil
	}
	return 0, errArg("unknown access level %q", name)
}

// glabResolveGroupID resolves group (a numeric ID or a "group/subgroup"
// path) to its numeric GitLab group ID — needed only where an endpoint's
// own request body has no path-string form (project creation's
// `namespace_id`, which GitLab's API only accepts as a plain integer;
// see gitlab_common.go's own doc comment on why every other call site
// uses glabEncodeID directly instead).
func glabResolveGroupID(ctx context.Context, conn remoteexec.Connection, group string) (int, error) {
	if n, err := strconv.Atoi(group); err == nil {
		return n, nil
	}
	var parsed struct {
		ID int `json:"id"`
	}
	res, err := glabAPIJSON(ctx, conn, "GET", "groups/"+glabEncodeID(group), nil, false, &parsed)
	if err != nil {
		return 0, err
	}
	if res.RC != 0 {
		return 0, fmt.Errorf("resolving group %q: %s", group, glabErrMsg(res))
	}
	return parsed.ID, nil
}

// glabResolveProjectID resolves project (a numeric ID or a
// "group/project" path) to its numeric GitLab project ID — needed only
// where an endpoint's own request body has no path-string form (the new
// runner-registration workflow's `project_id`, which GitLab's API only
// accepts as a plain integer; see gitlab_common.go's own doc comment on
// why every other call site uses glabEncodeID directly instead).
func glabResolveProjectID(ctx context.Context, conn remoteexec.Connection, project string) (int, error) {
	if n, err := strconv.Atoi(project); err == nil {
		return n, nil
	}
	var parsed struct {
		ID int `json:"id"`
	}
	res, err := glabAPIJSON(ctx, conn, "GET", "projects/"+glabEncodeID(project), nil, false, &parsed)
	if err != nil {
		return 0, err
	}
	if res.RC != 0 {
		return 0, fmt.Errorf("resolving project %q: %s", project, glabErrMsg(res))
	}
	return parsed.ID, nil
}

// glabResolveUserID resolves username to its numeric GitLab user ID via
// GET /users?username=<username>, matching real gitlab_* modules' own
// python-gitlab lookups (`gl.users.list(username=...)`), which look up
// by exact username the same way. Returns found=false (not an error) if
// no user has that exact username.
func glabResolveUserID(ctx context.Context, conn remoteexec.Connection, username string) (id int, found bool, err error) {
	var users []struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
	}
	res, err := glabAPIJSON(ctx, conn, "GET", "users?username="+url.QueryEscape(username), nil, false, &users)
	if err != nil {
		return 0, false, err
	}
	if res.RC != 0 {
		return 0, false, fmt.Errorf("looking up user %q: %s", username, glabErrMsg(res))
	}
	for _, u := range users {
		if u.Username == username {
			return u.ID, true, nil
		}
	}
	return 0, false, nil
}
