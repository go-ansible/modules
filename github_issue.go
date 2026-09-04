package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGithubIssue implements Ansible's `github_issue`
// (community.general) module: reads a GitHub issue's open/closed
// status, via `gh issue view` — see github_common.go's own doc comment
// for why this port substitutes the `gh` CLI for real github_issue's
// own direct GitHub REST API call (this module needs no auth argument
// at all, real or ported: `fetch_url` hits the public, unauthenticated
// REST endpoint directly, and `gh issue view` likewise works against a
// public repository without `gh` needing to be authenticated — though
// per github_common.go's own doc comment, an authenticated `gh` is
// still assumed as this batch's general precondition).
//
// Args: organization (required); repo (required); issue (required,
// int); action (choices: get_status, default get_status — the only
// choice the real module documents, kept here for argument-shape
// compatibility though it is never actually branched on, matching
// real github_issue.py's own `if action == "get_status" or action is
// None:` — the only code path that exists regardless of action's
// value).
//
// `gh issue view <issue> -R organization/repo --json state` decodes
// GitHub's own "OPEN"/"CLOSED" state text; Extra["issue_status"] is
// that value lower-cased ("open"/"closed"), matching real
// github_issue's own RETURN documentation sample exactly (GitHub's
// raw REST API also returns "open"/"closed" lower-case already; `gh`'s
// own --json state field instead renders GraphQL's upper-case
// IssueState enum, so this port lower-cases it to match real
// github_issue's own observed output rather than `gh`'s).
//
// A non-existent issue (`gh issue view` exits non-zero) is a Fail,
// matching real github_issue's own `if info["status"] == 404:
// module.fail_json(msg=f"Failed to find issue {issue}")`.
//
// Deviation — Changed=true always: real github_issue.py's own
// get_status branch calls `result.update(changed=True,
// issue_status=...)` even though this module only ever reads data and
// never modifies anything — an apparent bug in the real module (a
// read-only lookup reporting Changed=true), preserved here rather than
// "fixed", per this batch's own instruction to replicate real
// behavior faithfully (see github_issue.py's own source, read before
// porting, per this project's hard bibliography-before rule) rather
// than the more sensible Changed=false a read-only module would
// normally report elsewhere in this codebase.
func moduleGithubIssue(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	org, err := requireString(args, "organization")
	if err != nil {
		return Result{}, err
	}
	repo, err := requireString(args, "repo")
	if err != nil {
		return Result{}, err
	}
	if _, ok := args["issue"]; !ok {
		return Result{}, errArg("github_issue: missing required argument: issue")
	}
	issue := argInt(args, "issue", 0)
	spec := org + "/" + repo

	var v struct {
		State string `json:"state"`
	}
	res, err := ghRunJSON(ctx, conn, "", &v, "issue", "view", strconv.Itoa(issue), "-R", spec, "--json", "state")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("github_issue: Failed to find issue " + strconv.Itoa(issue)), nil
	}
	return Changed("").WithExtra("issue_status", strings.ToLower(v.State)), nil
}
