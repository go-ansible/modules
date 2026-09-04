package modules

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabMRObj is one entry of `glab mr list -F json`'s own JSON array,
// or `glab mr view -F json`'s own single object — only the fields this
// module reads.
type gitlabMRObj struct {
	IID                     int      `json:"iid"`
	Title                   string   `json:"title"`
	State                   string   `json:"state"`
	Description             string   `json:"description"`
	SourceBranch            string   `json:"source_branch"`
	TargetBranch            string   `json:"target_branch"`
	Labels                  []string `json:"labels"`
	ForceRemoveSourceBranch bool     `json:"force_remove_source_branch"`
	Assignees               []struct {
		Username string `json:"username"`
	} `json:"assignees"`
	Reviewers []struct {
		Username string `json:"username"`
	} `json:"reviewers"`
}

// moduleGitlabMergeRequest implements Ansible's `gitlab_merge_request`
// (community.general) module: creates, updates, or deletes a merge
// request.
//
// Unlike most of this batch's gitlab_* modules, `glab` DOES have a
// dedicated `mr create`/`list`/`view`/`update`/`delete` subcommand
// family, per this batch's own explicit instruction to use it — so this
// module drives those directly (via gitlab_dedicated_common.go's own
// glabCLI, shared with gitlab_deploy_key.go/gitlab_issue.go) instead of
// `glab api`. The one gap: `glab mr update` has no flag for
// remove_source_branch at all (verified against docs.gitlab.com/cli/
// mr/update/ — create has `--remove-source-branch`, update does not),
// so a remove_source_branch CHANGE on an existing merge request falls
// back to `glab api PUT .../merge_requests/:iid -f
// remove_source_branch=...` — the same "prefer-dedicated, fall back to
// `glab api` for what it can't do" pattern gitlab_deploy_key.go/
// gitlab_issue.go document. See gitlab_common.go's own doc comment for
// the accepted-but-inert api_*/validate_certs/ca_path arguments this
// module (like every other one in this batch) does not wire in.
//
// Args: project (required); source_branch/target_branch/title (all
// required — real gitlab_merge_request's own matching key, together
// with state_filter); description; description_path — read ON THE
// TARGET via `cat` over Exec, same reasoning and same-file semantics as
// gitlab_issue.go's own description_path; labels/assignee_ids/
// reviewer_ids — real gitlab_merge_request's own doc types these as
// COMMA-SEPARATED STRINGS (not lists, unlike gitlab_issue's own labels/
// assignee_ids), so this port splits on "," and trims whitespace around
// each entry before diffing; an empty string clears the set, matching
// real gitlab_merge_request's own documented "Set to empty string to
// unassign all"; remove_source_branch (bool, default false);
// state (present|absent, default present); state_filter (opened|
// closed|locked|merged, default opened).
//
// Existing merge requests are found via `glab mr list --all -F json`
// (fetching every state, since `glab mr list` has no single flag
// covering all four of opened/closed/locked/merged at once — verified
// against docs.gitlab.com/cli/mr/list/), filtered client-side to an
// EXACT title+source_branch+target_branch+state_filter match — matching
// real gitlab_merge_request's own documented "Existing merge requests
// are matched based on title, source_branch, target_branch, and
// state_filter filters" and "When multiple merge requests are detected,
// the task fails" (Result{Failed:true}, an expected, well-formed
// refusal, not an infrastructure error).
//
// state=absent: a match is deleted (`glab mr delete`); no match is a
// no-op. state=present: no match -> `glab mr create` (source_branch is
// used here only; real gitlab_merge_request's own doc notes it is
// "Ignored while updating existing merge request", matching `glab mr
// update`'s own lack of a source-branch flag). A match is updated
// (`glab mr update`, plus the remove_source_branch `glab api` fallback
// above) only if description/labels/assignees/reviewers/target_branch/
// remove_source_branch actually differ, else left untouched.
//
// Extra["mr"]: the merge request object as it now stands (`glab mr view
// -F json`), present on every non-error outcome except a plain no-op
// delete, matching real gitlab_merge_request's own documented return.
func moduleGitlabMergeRequest(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_merge_request"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	sourceBranch, err := requireString(args, "source_branch")
	if err != nil {
		return Result{}, err
	}
	targetBranch, err := requireString(args, "target_branch")
	if err != nil {
		return Result{}, err
	}
	title, err := requireString(args, "title")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_merge_request: state must be one of present, absent, got %q", state)
	}
	stateFilter := argString(args, "state_filter", "opened")
	switch stateFilter {
	case "opened", "closed", "locked", "merged":
	default:
		return Result{}, errArg("gitlab_merge_request: state_filter must be one of opened, closed, locked, merged, got %q", stateFilter)
	}

	lres, err := glabCLI(ctx, conn, nil, "glab", "mr", "list", "-R", project, "--all", "-F", "json", "--per-page", "100")
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_merge_request: unable to list merge requests: " + strings.TrimSpace(lres.Stderr)), nil
	}
	var candidates []gitlabMRObj
	if strings.TrimSpace(lres.Stdout) != "" {
		if err := json.Unmarshal([]byte(lres.Stdout), &candidates); err != nil {
			return Result{}, err
		}
	}
	var matches []gitlabMRObj
	for _, c := range candidates {
		if c.Title == title && c.SourceBranch == sourceBranch && c.TargetBranch == targetBranch && c.State == stateFilter {
			matches = append(matches, c)
		}
	}
	if len(matches) > 1 {
		return Fail("gitlab_merge_request: multiple merge requests match title " + title), nil
	}
	var existing *gitlabMRObj
	if len(matches) == 1 {
		existing = &matches[0]
	}

	if state == "absent" {
		if existing == nil {
			return Ok(title + " already absent"), nil
		}
		dres, err := glabCLI(ctx, conn, nil, "glab", "mr", "delete", strconv.Itoa(existing.IID), "-R", project)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_merge_request: unable to delete " + title + ": " + strings.TrimSpace(dres.Stderr)), nil
		}
		return Changed(title + " deleted"), nil
	}

	description := argString(args, "description", "")
	if p := argString(args, "description_path", ""); p != "" {
		if content, err := run(ctx, conn, "cat "+shellQuote(p)); err == nil {
			description = content
		}
	}
	removeSourceBranch := argBool(args, "remove_source_branch", false)

	var iid int
	changed := false

	if existing == nil {
		argv := []string{"glab", "mr", "create", "-R", project, "-s", sourceBranch, "-b", targetBranch, "-t", title, "--yes"}
		if description != "" {
			argv = append(argv, "-d", description)
		}
		for _, l := range splitCommaList(argString(args, "labels", "")) {
			argv = append(argv, "-l", l)
		}
		for _, a := range splitCommaList(argString(args, "assignee_ids", "")) {
			argv = append(argv, "-a", a)
		}
		for _, r := range splitCommaList(argString(args, "reviewer_ids", "")) {
			argv = append(argv, "--reviewer", r)
		}
		if removeSourceBranch {
			argv = append(argv, "--remove-source-branch")
		}
		cres, err := glabCLI(ctx, conn, nil, argv...)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return Fail("gitlab_merge_request: unable to create " + title + ": " + strings.TrimSpace(cres.Stderr)), nil
		}
		changed = true
		created, ferr := gitlabFindMR(ctx, conn, project, title, sourceBranch, targetBranch, "opened")
		if ferr != nil {
			return Result{}, ferr
		}
		if created == nil {
			return Fail("gitlab_merge_request: created " + title + " but could not find it afterward"), nil
		}
		iid = created.IID
	} else {
		iid = existing.IID
		argv := []string{"glab", "mr", "update", strconv.Itoa(iid), "-R", project}
		diff := false
		if _, ok := args["description"]; ok || argString(args, "description_path", "") != "" {
			if description != existing.Description {
				argv = append(argv, "-d", description)
				diff = true
			}
		}
		if _, ok := args["labels"]; ok {
			add, remove := stringSetDiff(splitCommaList(argString(args, "labels", "")), existing.Labels)
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
			cur := make([]string, 0, len(existing.Assignees))
			for _, a := range existing.Assignees {
				cur = append(cur, a.Username)
			}
			desired := splitCommaList(argString(args, "assignee_ids", ""))
			if len(desired) == 0 && len(cur) > 0 {
				argv = append(argv, "--unassign")
				diff = true
			} else {
				add, remove := stringSetDiff(desired, cur)
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
		if _, ok := args["reviewer_ids"]; ok {
			cur := make([]string, 0, len(existing.Reviewers))
			for _, r := range existing.Reviewers {
				cur = append(cur, r.Username)
			}
			desired := splitCommaList(argString(args, "reviewer_ids", ""))
			add, remove := stringSetDiff(desired, cur)
			for _, r := range add {
				argv = append(argv, "-r", r)
				diff = true
			}
			for _, r := range remove {
				argv = append(argv, "-r", "-"+r)
				diff = true
			}
		}
		if targetBranch != existing.TargetBranch {
			argv = append(argv, "--target-branch", targetBranch)
			diff = true
		}
		if diff {
			ures, err := glabCLI(ctx, conn, nil, argv...)
			if err != nil {
				return Result{}, err
			}
			if ures.RC != 0 {
				return Fail("gitlab_merge_request: unable to update " + title + ": " + strings.TrimSpace(ures.Stderr)), nil
			}
			changed = true
		}
		if removeSourceBranch != existing.ForceRemoveSourceBranch {
			body := map[string]any{"remove_source_branch": removeSourceBranch}
			rres, err := glabAPIJSON(ctx, conn, "PUT", "projects/"+glabEncodeID(project)+"/merge_requests/"+strconv.Itoa(iid), body, false, nil)
			if err != nil {
				return Result{}, err
			}
			if rres.RC != 0 {
				return Fail("gitlab_merge_request: unable to update remove_source_branch for " + title + ": " + glabErrMsg(rres)), nil
			}
			changed = true
		}
	}

	final, ferr := gitlabMRView(ctx, conn, project, iid)
	if ferr != nil {
		return Result{}, ferr
	}
	r := Result{Changed: changed}
	if final != nil {
		r = r.WithExtra("mr", *final)
	}
	if changed {
		r.Msg = title + " created or updated"
	} else {
		r.Msg = title + " already up to date"
	}
	return r, nil
}

// gitlabFindMR re-lists merge requests (matching moduleGitlabMergeRequest's
// own list-then-exact-match approach) to recover the IID of a merge
// request this module just created — `glab mr create` prints a URL, not
// structured JSON, on stdout.
func gitlabFindMR(ctx context.Context, conn remoteexec.Connection, project, title, sourceBranch, targetBranch, stateFilter string) (*gitlabMRObj, error) {
	res, err := glabCLI(ctx, conn, nil, "glab", "mr", "list", "-R", project, "--all", "-F", "json", "--per-page", "100")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var candidates []gitlabMRObj
	if strings.TrimSpace(res.Stdout) != "" {
		if err := json.Unmarshal([]byte(res.Stdout), &candidates); err != nil {
			return nil, err
		}
	}
	for i := range candidates {
		c := candidates[i]
		if c.Title == title && c.SourceBranch == sourceBranch && c.TargetBranch == targetBranch && c.State == stateFilter {
			return &candidates[i], nil
		}
	}
	return nil, nil
}

// gitlabMRView runs `glab mr view <iid> -R project -F json`, decoding
// the single merge request object.
func gitlabMRView(ctx context.Context, conn remoteexec.Connection, project string, iid int) (*gitlabMRObj, error) {
	res, err := glabCLI(ctx, conn, nil, "glab", "mr", "view", strconv.Itoa(iid), "-R", project, "-F", "json")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var obj gitlabMRObj
	if err := json.Unmarshal([]byte(res.Stdout), &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

// splitCommaList splits a real gitlab_merge_request-style comma-
// separated argument (labels/assignee_ids/reviewer_ids) into trimmed,
// non-empty entries.
func splitCommaList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
