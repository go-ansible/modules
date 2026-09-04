package modules

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabIssueObj is one entry of `glab issue list -F json`'s own JSON
// array, or `glab issue view -F json`'s own single object — only the
// fields this module reads.
type gitlabIssueObj struct {
	IID         int      `json:"iid"`
	Title       string   `json:"title"`
	State       string   `json:"state"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	Assignees   []struct {
		Username string `json:"username"`
	} `json:"assignees"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
}

// moduleGitlabIssue implements Ansible's `gitlab_issue`
// (community.general) module: creates, updates, or deletes a GitLab
// issue.
//
// Unlike most of this batch's gitlab_* modules, `glab` DOES have a
// dedicated `issue create`/`list`/`view`/`update`/`close`/`delete`
// subcommand family, per this batch's own explicit instruction to use
// it — so this module drives those directly (via
// gitlab_dedicated_common.go's own glabCLI, shared with
// gitlab_deploy_key.go/gitlab_merge_request.go) instead of `glab api`.
// The one gap: `glab issue create`/`update` have no flag for
// issue_type (verified against docs.gitlab.com/cli/issue/{create,
// update}/ — neither page lists one, unlike gitlab_deploy_key's
// can_push, which the dedicated command DOES cover for create but not
// update), so a non-default issue_type falls back to `glab api PUT
// .../issues/:iid -f issue_type=...` after the dedicated create/update
// call — the same "prefer-dedicated, fall back to `glab api` for what
// it can't do" pattern gitlab_deploy_key.go documents. See
// gitlab_common.go's own doc comment for the accepted-but-inert
// api_*/validate_certs/ca_path arguments this module (like every other
// one in this batch) does not wire in.
//
// Args: project (required); title (required) — this port's (and real
// gitlab_issue's own, per its own doc: "used as a unique identifier to
// ensure idempotency") matching key; description; description_path — a
// path READ ON THE TARGET (this port's modules reach the target only
// through the Connection, see module.go's own doc comment; real
// gitlab_issue's own Python module runs ON the managed node too and
// reads this path locally there, so this is the same file, just read
// via `cat` over Exec rather than Python's own open()) — overrides
// description when it can actually be read (a missing file is silently
// ignored, description is left as given, matching real gitlab_issue's
// own "if found" wording); labels ([]string) — omitted entirely leaves
// existing labels untouched, an empty list clears them, matching real
// gitlab_issue's own documented "Set to an empty array to remove all
// labels"; assignee_ids ([]string of usernames) — same
// omitted/empty-clears semantics; milestone_search (a milestone title;
// empty string unassigns); milestone_group_id — accepted, NOT wired:
// `glab issue create/update`'s own `-m/--milestone` flag resolves a
// milestone by title within the CURRENT project only, with no group-
// scoped-milestone equivalent this port could drive (a documented,
// honest gap, not a silent misinterpretation); issue_type (issue|
// incident|test_case, default issue) — see the `glab api` fallback
// above; state (present|absent, default present); state_filter
// (opened|closed, default opened) — which existing issues (by state)
// this module's own title search considers a match.
//
// Existing issues are found via `glab issue list --search <title> --in
// title [-c for state_filter=closed]`, then filtered to an EXACT title
// match client-side (glab's own --search is a substring match, not
// exact) — matching real gitlab_issue's own documented "Existing issues
// are matched based on title and state_filter filters" and "When
// multiple issues are detected, the task fails" (Result{Failed:true}
// here, an expected, well-formed refusal, not an infrastructure error).
//
// state=absent: a match is permanently deleted (`glab issue delete`,
// not merely closed — matching real gitlab_issue's own documented
// state=absent semantics exactly); no match is a no-op. state=present:
// no match -> `glab issue create`; a match is updated (`glab issue
// update`) only if description/labels/assignees/milestone actually
// differ, else left untouched.
//
// Extra["issue"]: the issue object as it now stands (`glab issue view
// -F json`), present on every non-error outcome except a plain no-op
// delete, matching real gitlab_issue's own documented return.
func moduleGitlabIssue(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_issue"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	title, err := requireString(args, "title")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_issue: state must be one of present, absent, got %q", state)
	}
	stateFilter := argString(args, "state_filter", "opened")
	if stateFilter != "opened" && stateFilter != "closed" {
		return Result{}, errArg("gitlab_issue: state_filter must be one of opened, closed, got %q", stateFilter)
	}

	listArgv := []string{"glab", "issue", "list", "-R", project, "--search", title, "--in", "title", "-F", "json", "--per-page", "100"}
	if stateFilter == "closed" {
		listArgv = append(listArgv, "-c")
	}
	lres, err := glabCLI(ctx, conn, nil, listArgv...)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_issue: unable to list issues: " + strings.TrimSpace(lres.Stderr)), nil
	}
	var candidates []gitlabIssueObj
	if strings.TrimSpace(lres.Stdout) != "" {
		if err := json.Unmarshal([]byte(lres.Stdout), &candidates); err != nil {
			return Result{}, err
		}
	}
	var matches []gitlabIssueObj
	for _, c := range candidates {
		if c.Title == title {
			matches = append(matches, c)
		}
	}
	if len(matches) > 1 {
		return Fail("gitlab_issue: multiple issues match title " + title), nil
	}
	var existing *gitlabIssueObj
	if len(matches) == 1 {
		existing = &matches[0]
	}

	if state == "absent" {
		if existing == nil {
			return Ok(title + " already absent"), nil
		}
		dres, err := glabCLI(ctx, conn, nil, "glab", "issue", "delete", strconv.Itoa(existing.IID), "-R", project)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_issue: unable to delete " + title + ": " + strings.TrimSpace(dres.Stderr)), nil
		}
		return Changed(title + " deleted"), nil
	}

	description := argString(args, "description", "")
	if p := argString(args, "description_path", ""); p != "" {
		if content, err := run(ctx, conn, "cat "+shellQuote(p)); err == nil {
			description = content
		}
	}
	issueType := argString(args, "issue_type", "issue")

	var iid int
	changed := false

	if existing == nil {
		argv := []string{"glab", "issue", "create", "-R", project, "-t", title, "--yes"}
		if description != "" {
			argv = append(argv, "-d", description)
		}
		for _, l := range argStringList(args, "labels") {
			argv = append(argv, "-l", l)
		}
		for _, a := range argStringList(args, "assignee_ids") {
			argv = append(argv, "-a", a)
		}
		if _, ok := args["milestone_search"]; ok {
			argv = append(argv, "-m", argString(args, "milestone_search", ""))
		}
		cres, err := glabCLI(ctx, conn, nil, argv...)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("gitlab_issue: unable to create " + title + ": " + strings.TrimSpace(cres.Stderr)), nil
		}
		changed = true
		created, ferr := gitlabFindIssueByTitle(ctx, conn, project, title, "opened")
		if ferr != nil {
			return Result{}, ferr
		}
		if created == nil {
			return Fail("gitlab_issue: created " + title + " but could not find it afterward"), nil
		}
		iid = created.IID
	} else {
		iid = existing.IID
		argv := []string{"glab", "issue", "update", strconv.Itoa(iid), "-R", project}
		diff := false
		if _, ok := args["description"]; ok || argString(args, "description_path", "") != "" {
			if description != existing.Description {
				argv = append(argv, "-d", description)
				diff = true
			}
		}
		if _, ok := args["labels"]; ok {
			add, remove := stringSetDiff(argStringList(args, "labels"), existing.Labels)
			for _, l := range add {
				argv = append(argv, "-l", l)
				diff = true
			}
			for _, l := range remove {
				argv = append(argv, "-u", l)
				diff = true
			}
		}
		if _, ok := args["assignee_ids"]; ok {
			curAssignees := make([]string, 0, len(existing.Assignees))
			for _, a := range existing.Assignees {
				curAssignees = append(curAssignees, a.Username)
			}
			desired := argStringList(args, "assignee_ids")
			add, remove := stringSetDiff(desired, curAssignees)
			if len(desired) == 0 && len(curAssignees) > 0 {
				argv = append(argv, "--unassign")
				diff = true
			} else {
				for _, a := range add {
					argv = append(argv, "-a", a)
					diff = true
				}
				for _, a := range remove {
					argv = append(argv, "-a", "-"+a)
					diff = true
				}
			}
		}
		if _, ok := args["milestone_search"]; ok {
			want := argString(args, "milestone_search", "")
			have := ""
			if existing.Milestone != nil {
				have = existing.Milestone.Title
			}
			if want != have {
				argv = append(argv, "-m", want)
				diff = true
			}
		}
		if diff {
			ures, err := glabCLI(ctx, conn, nil, argv...)
			if err != nil {
				return Result{}, err
			}
			if ures.RC != 0 {
				return Fail("gitlab_issue: unable to update " + title + ": " + strings.TrimSpace(ures.Stderr)), nil
			}
			changed = true
		}
	}

	if issueType != "issue" {
		curType, terr := gitlabIssueTypeOf(ctx, conn, project, iid)
		if terr != nil {
			return Result{}, terr
		}
		if curType != issueType {
			body := map[string]any{"issue_type": issueType}
			tres, err := glabAPIJSON(ctx, conn, "PUT", "projects/"+glabEncodeID(project)+"/issues/"+strconv.Itoa(iid), body, false, nil)
			if err != nil {
				return Result{}, err
			}
			if tres.RC != 0 {
				return Fail("gitlab_issue: unable to set issue_type for " + title + ": " + glabErrMsg(tres)), nil
			}
			changed = true
		}
	}

	final, ferr := gitlabIssueView(ctx, conn, project, iid)
	if ferr != nil {
		return Result{}, ferr
	}
	r := Result{Changed: changed}
	if final != nil {
		r = r.WithExtra("issue", *final)
	}
	if changed {
		r.Msg = title + " created or updated"
	} else {
		r.Msg = title + " already up to date"
	}
	return r, nil
}

// gitlabFindIssueByTitle re-lists issues (matching moduleGitlabIssue's
// own list-then-exact-title-filter approach) to recover the IID of an
// issue this module just created — `glab issue create` prints a URL,
// not structured JSON, on stdout, so this is how this port learns the
// new issue's IID.
func gitlabFindIssueByTitle(ctx context.Context, conn remoteexec.Connection, project, title, stateFilter string) (*gitlabIssueObj, error) {
	argv := []string{"glab", "issue", "list", "-R", project, "--search", title, "--in", "title", "-F", "json", "--per-page", "100"}
	if stateFilter == "closed" {
		argv = append(argv, "-c")
	}
	res, err := glabCLI(ctx, conn, nil, argv...)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var candidates []gitlabIssueObj
	if strings.TrimSpace(res.Stdout) != "" {
		if err := json.Unmarshal([]byte(res.Stdout), &candidates); err != nil {
			return nil, err
		}
	}
	for i := range candidates {
		if candidates[i].Title == title {
			return &candidates[i], nil
		}
	}
	return nil, nil
}

// gitlabIssueView runs `glab issue view <iid> -R project -F json`,
// decoding the single issue object.
func gitlabIssueView(ctx context.Context, conn remoteexec.Connection, project string, iid int) (*gitlabIssueObj, error) {
	res, err := glabCLI(ctx, conn, nil, "glab", "issue", "view", strconv.Itoa(iid), "-R", project, "-F", "json")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var obj gitlabIssueObj
	if err := json.Unmarshal([]byte(res.Stdout), &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// gitlabIssueTypeOf reads one issue's own issue_type via `glab api`
// (see moduleGitlabIssue's own doc comment on why: neither `glab issue
// create` nor `update` exposes this field, so it must be read/written
// through the fallback).
func gitlabIssueTypeOf(ctx context.Context, conn remoteexec.Connection, project string, iid int) (string, error) {
	var obj struct {
		IssueType string `json:"issue_type"`
	}
	res, err := glabAPIJSON(ctx, conn, "GET", "projects/"+glabEncodeID(project)+"/issues/"+strconv.Itoa(iid), nil, false, &obj)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	return obj.IssueType, nil
}

// stringSetDiff returns (add, remove): entries in desired not in
// current, and entries in current not in desired — used by
// moduleGitlabIssue/moduleGitlabMergeRequest to compute the minimal
// `glab issue update`/`glab mr update` add/remove flags needed to make
// current match desired.
func stringSetDiff(desired, current []string) (add, remove []string) {
	curSet := map[string]bool{}
	for _, c := range current {
		curSet[c] = true
	}
	desSet := map[string]bool{}
	for _, d := range desired {
		desSet[d] = true
		if !curSet[d] {
			add = append(add, d)
		}
	}
	for _, c := range current {
		if !desSet[c] {
			remove = append(remove, c)
		}
	}
	sort.Strings(add)
	sort.Strings(remove)
	return add, remove
}
