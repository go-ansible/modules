package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJira implements (a partial-fidelity subset of) Ansible's `jira`
// module via Atlassian's own official `acli` (Atlassian Command Line
// Interface, "acli jira", launched 2026 — developer.atlassian.com/
// cloud/acli/reference/commands/jira/) instead of real jira.py's own
// hand-rolled `fetch_url` calls straight against the JIRA REST API —
// the same "shell out to the platform's own official CLI instead of an
// API client" precedent this port already uses elsewhere (see
// github_common.go's own doc comment for the fuller rationale).
//
// # acli is Jira CLOUD only — real jira.py's own Server/Data Center
// # support has no equivalent in this port at all
//
// acli is documented by Atlassian itself as the CLI "for Jira Cloud".
// Real jira.py talks to ANY Jira instance reachable at its own `uri`
// argument, cloud or self-hosted, and even has a dedicated `cloud` bool
// argument specifically to pick between the two REST search endpoints.
// `acli` has no concept of an arbitrary self-hosted `uri` at all: it
// authenticates against one Jira Cloud SITE at a time via a prior `acli
// jira auth login --site <site> --email <email> --token` (verified
// directly in acli's own jira-auth-login reference page — token is read
// from STDIN via its own `--token` flag, never a command-line value or
// an environment variable this port could lean on), and every `acli
// jira workitem` command operates against whichever site that prior
// login selected. So: this module's own `uri`/`username`/`password`/
// `token`/`client_cert`/`client_key`/`validate_certs`/`timeout`/`cloud`
// arguments are ALL accepted, for argument-shape compatibility with real
// playbooks written against real jira.py, but have NO EFFECT — matching
// the precedent ipa_common.go's own doc comment already sets for a CLI
// with no per-invocation credential concept. A prior `acli jira auth
// login` for the correct site must already have been run on the target;
// this port does not manage that login itself, and cannot detect or
// correct a mismatch between a task's own `uri` and whichever site is
// currently logged in. A Jira Server/Data Center target (cloud=false)
// simply cannot be reached through this module at all — there is no
// substitute CLI surface for it here.
//
// # Operation mapping — verified directly against acli's own reference
// # pages (developer.atlassian.com/cloud/acli/reference/commands/
// # jira-workitem*) and, for exact flag definitions with no ambiguity,
// # acli's own documented flag tables — not guessed from the module name
//
// Real jira.py's own `operation` (aliased `command`) choices are attach,
// comment, create, edit, fetch, link, search, transition, update,
// worklog. Coverage, one operation at a time:
//
//   - create -> `acli jira workitem create --project P --summary S
//     --type T [--description D] [--assignee A] [--label L1,L2] --json`.
//     acli create's own fixed flag set (verified: -p/--project,
//     -s/--summary, -t/--type, -d/--description, -a/--assignee,
//     -l/--label, --parent) covers real jira.py's own project/summary/
//     issuetype/description/assignee/account_id arguments exactly
//     (account_id is passed through the same --assignee flag, matching
//     acli's own "by email or account ID" description). Real jira.py's
//     own `fields` dict is merged OVER the top-level arguments
//     ("createfields.update(self.vars.fields)", applied after
//     project/summary/issuetype/description are set) — this port
//     mirrors that precedence for the same-named keys fields[summary]/
//     fields[description]/fields[issuetype]/fields[labels] (each
//     overriding its top-level argument when both are given), plus
//     fields[assignee] via jiraAssignee. Any OTHER `fields` key
//     (arbitrary JIRA fields, e.g. `customfield_13225`, or `reporter`)
//     has NO acli create equivalent beyond the fixed flags above — acli
//     DOES have a `--from-json`/`--generate-json` full-JSON-body path,
//     but this port has no live acli binary to capture
//     --generate-json's own template shape against and confirm it
//     accepts arbitrary `fields`-style keys rather than just acli's own
//     fixed set encoded as JSON, so this port does NOT guess at that
//     shape — any such key makes this operation Fail loud, naming the
//     unsupported keys, rather than silently dropping them.
//   - comment -> `acli jira workitem comment create --key ISSUE --body
//     TEXT --json`. Real jira.py's own `comment_visibility` argument
//     (restricting a comment to a group/role) has no equivalent on
//     `comment create` itself — acli's own comment-visibility
//     functionality is a SEPARATE subcommand
//     (`workitem comment visibility`) that only applies to an ALREADY
//     created comment, not an atomic create-with-visibility call — so
//     comment_visibility is Fail loud, not silently ignored, when
//     given. Real jira.py's own `fields` (merged into the comment body
//     for arbitrary top-level keys, e.g. Jira Service Management
//     internal-comment properties) has no acli equivalent either — Fail
//     loud if non-empty.
//   - edit -> `acli jira workitem edit --key ISSUE [--summary S]
//     [--description D] [--assignee A | --remove-assignee] [--labels L]
//     [--type T] --yes --json`. Real jira.py's own operation_edit PUTs
//     `fields` WHOLESALE and unconditionally (any JIRA field at all);
//     acli edit only exposes summary/description/assignee/labels/type as
//     fixed flags (verified against acli's own jira-workitem-edit
//     reference page) — this port maps exactly those keys out of
//     `fields` (plus assignee/account_id) and Fails loud, naming the
//     unsupported keys, for anything else in `fields`.
//   - update -> Fail loud, always. Real jira.py's own operation_update
//     sends `{"update": fields}` — Jira's own generic add/set/remove
//     field-operation envelope, letting a caller do things no fixed CLI
//     flag can express (e.g. `{"labels": [{"add": "x"}]}`). acli has no
//     generic field-operations primitive anywhere in its `workitem`
//     command group (verified: create/edit both only ever fully
//     REPLACE a fixed set of named fields) — there is no way to compose
//     an arbitrary add/set/remove operation through acli at all, so this
//     port does not attempt a partial or guessed mapping.
//   - fetch -> `acli jira workitem view ISSUE --fields '*all' --json`
//     (view's own key is POSITIONAL, verified against acli's own
//     jira-workitem-view reference and community examples — unlike
//     every other workitem subcommand below, which take `-k/--key`).
//     `--fields '*all'` requests every field, matching a raw REST GET
//     issue's own default full-fields response as closely as acli's
//     own documented field-selection syntax allows (view's OWN default
//     is a small fixed subset: key,issuetype,summary,status,assignee,
//     description).
//   - link -> `acli jira workitem link create --out OUTWARD --in INWARD
//     --type LINKTYPE --yes --json` — a full-fidelity mapping, verified
//     against acli's own jira-workitem-link-create reference page:
//     --out/--in/--type correspond exactly to real jira.py's own
//     outwardissue/inwardissue/linktype.
//   - search -> `acli jira workitem search --jql JQL [--limit N]
//     [--fields f1,f2,...] --json`. Real jira.py's own `fields` dict
//     (interpreted as a REST-search fields FILTER, not a body to merge)
//     maps onto acli search's own `-f/--fields` comma-separated field
//     list; `maxresults` maps onto `--limit`. Real jira.py's own `cloud`
//     bool switches between two different REST search endpoints
//     (Server's legacy /search vs Cloud's /search/jql) — acli, being a
//     Cloud-only tool, always effectively behaves as cloud=true; no
//     separate acli invocation shape exists for cloud=false, consistent
//     with this module's own Cloud-only limitation above.
//   - transition -> `acli jira workitem transition --key ISSUE --status
//     STATUS --yes --json`. Real jira.py's own `status_id` argument
//     (selecting a transition by ID rather than name) has no VERIFIED
//     acli equivalent — acli transition's own documented flag set has
//     exactly one selector, `-s/--status`, described only as "Status to
//     transition the work item" with no confirmation it also accepts a
//     numeric transition ID, and this port does not guess. status_id
//     therefore Fails loud rather than being silently passed through
//     --status on an unverified hope it also accepts IDs. Real jira.py's
//     own `comment`/`summary`/`description`/arbitrary `fields` on a
//     transition are applied ATOMICALLY in the SAME REST call as the
//     transition; acli transition has no flags for any of them. This
//     port instead performs a best-effort, NON-ATOMIC sequence: the
//     transition via `acli jira workitem transition`, then (only if it
//     succeeded) a SEPARATE `acli jira workitem comment create` for
//     `comment` if given. summary/description/other `fields` given
//     alongside operation=transition Fail loud (naming them) rather
//     than being silently dropped — acli exposes no way to change them
//     as part of, or immediately after, a transition through this
//     command group.
//   - attach -> Fail loud, always. acli's own `workitem attachment`
//     command group only has `list`/`delete` (verified directly against
//     acli's own reference pages and a GitHub search across community
//     acli command references) — there is no `attachment create`/
//     `upload`/`add` verb anywhere in acli to upload a new file to a
//     work item, and (matching update's own situation) no generic
//     passthrough either.
//   - worklog -> Fail loud, always. acli's own `workitem` command group
//     has no worklog-related subcommand of any kind (verified: the full
//     subcommand list — archive/assign/attachment-*/clone/comment-*/
//     create/create-bulk/delete/edit/link/search/transition/unarchive/
//     view/watcher-remove — contains nothing worklog-shaped), and no
//     other `acli jira` command group (auth/board/dashboard/field/
//     filter/project/sprint) covers it either.
//
// Every Fail above is a Result{Failed:true}, not a Go error — the
// request itself is well-formed and a human operator hitting this exact
// wall from a terminal would see the identical absence of any acli
// command to run; it is this port's own architecture (shelling out to
// acli specifically) that cannot satisfy it, honestly reported rather
// than silently skipped or faked as a no-op success.
//
// # Output
//
// Every successful acli invocation's own `--json` stdout is decoded and
// returned as Extra["meta"] — the closest equivalent to real jira.py's
// own `meta` return value, though NOT the same shape: real jira.py's
// `meta` is the raw Jira REST API response body for that call, while
// acli's own `--json` output is acli's OWN result envelope, verified to
// exist as a documented flag on every workitem subcommand this module
// uses but not byte-for-byte compared against a live Jira Cloud REST
// response — a caller depending on a specific `meta.<field>` path
// written against real jira.py (e.g. `meta.fields.creator.displayName`
// from real jira.py's own EXAMPLES) should not assume this port's
// `meta` has the identical shape.
func moduleJira(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	op := argString(args, "operation", argString(args, "command", ""))
	if op == "" {
		return Result{}, errArg("jira: missing required argument: operation")
	}

	if _, err := run(ctx, conn, "command -v acli"); err != nil {
		return Fail("jira: the acli binary (Atlassian's own official CLI for Jira Cloud) is required on the " +
			"target and was not found in PATH — this port shells out to it rather than speaking the JIRA " +
			"REST API directly; see this module's own doc comment, including the precondition that `acli jira " +
			"auth login` must already have been run for the correct site"), nil
	}

	fields := jiraFieldsMap(args)

	switch op {
	case "create":
		return jiraOpCreate(ctx, conn, args, fields)
	case "comment":
		return jiraOpComment(ctx, conn, args, fields)
	case "edit":
		return jiraOpEdit(ctx, conn, args, fields)
	case "update":
		return Fail("jira: operation=update has no acli equivalent — acli's own workitem command group has no " +
			"generic add/set/remove field-operations primitive (create/edit both only ever fully replace a " +
			"fixed set of named fields); see this module's own doc comment"), nil
	case "fetch":
		return jiraOpFetch(ctx, conn, args)
	case "link":
		return jiraOpLink(ctx, conn, args)
	case "search":
		return jiraOpSearch(ctx, conn, args, fields)
	case "transition":
		return jiraOpTransition(ctx, conn, args, fields)
	case "attach":
		return Fail("jira: operation=attach has no acli equivalent — acli's own workitem attachment command " +
			"group only has list/delete, no create/upload verb; see this module's own doc comment"), nil
	case "worklog":
		return Fail("jira: operation=worklog has no acli equivalent — no acli jira command group (workitem or " +
			"otherwise) exposes any worklog-related subcommand; see this module's own doc comment"), nil
	default:
		return Result{}, errArg("jira: unknown operation %q", op)
	}
}

func jiraFieldsMap(args map[string]any) map[string]any {
	if v, ok := args["fields"]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

// jiraUnsupportedFields returns an error naming any key of fields not in
// handled, for the "Fail loud rather than silently drop" contract this
// module's own doc comment describes for create/edit/comment/transition.
func jiraUnsupportedFields(moduleOp string, fields map[string]any, handled ...string) error {
	handledSet := map[string]bool{}
	for _, h := range handled {
		handledSet[h] = true
	}
	var extra []string
	for k := range fields {
		if !handledSet[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return fmt.Errorf("jira: operation=%s: fields %v have no acli equivalent (see this module's own doc comment)", moduleOp, extra)
}

func jiraAssignee(args map[string]any, fields map[string]any) string {
	if v := argString(args, "account_id", ""); v != "" {
		return v
	}
	if v := argString(args, "assignee", ""); v != "" {
		return v
	}
	if v, ok := fields["assignee"]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

// jiraFieldStr returns fields[key] rendered as a string, and whether it
// was present at all — used by create/edit to let an explicit `fields`
// entry override (or, for edit, BE the only source of) the
// correspondingly-named top-level argument, matching real jira.py's own
// `createfields.update(self.vars.fields)` (create) / `{"fields":
// self.vars.fields}` (edit, which never even reads the top-level
// summary/description arguments at all).
func jiraFieldStr(fields map[string]any, key string) (string, bool) {
	v, ok := fields[key]
	if !ok {
		return "", false
	}
	return fmt.Sprint(v), true
}

func jiraRunJSON(ctx context.Context, conn remoteexec.Connection, argv ...string) (map[string]any, remoteexec.Result, error) {
	argv = append(argv, "--json")
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	res, err := runStatus(ctx, conn, strings.Join(quoted, " "))
	if err != nil {
		return nil, res, err
	}
	if res.RC != 0 {
		return nil, res, nil
	}
	out := map[string]any{}
	if strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), &out); jerr != nil {
			// acli's own --json output not parsing as JSON isn't fatal to
			// the caller's already-successful invocation; the raw text
			// is still surfaced.
			out = map[string]any{"raw": res.Stdout}
		}
	}
	return out, res, nil
}

func jiraErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

func jiraOpCreate(ctx context.Context, conn remoteexec.Connection, args map[string]any, fields map[string]any) (Result, error) {
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, errArg("jira: create: %v", err)
	}
	summary, err := requireString(args, "summary")
	if err != nil {
		return Result{}, errArg("jira: create: %v", err)
	}
	issuetype, err := requireString(args, "issuetype")
	if err != nil {
		return Result{}, errArg("jira: create: %v", err)
	}
	if uerr := jiraUnsupportedFields("create", fields, "assignee", "reporter", "summary", "description", "issuetype", "labels"); uerr != nil {
		return Result{}, uerr
	}
	if _, ok := fields["reporter"]; ok {
		return Result{}, errArg("jira: create: fields[reporter] has no acli equivalent (see this module's own doc comment)")
	}

	// fields, when it also sets these same-named keys, OVERRIDES the
	// top-level argument — matching real jira.py's own
	// `createfields.update(self.vars.fields)`, applied after the
	// top-level project/summary/issuetype/description are set.
	if v, ok := jiraFieldStr(fields, "summary"); ok {
		summary = v
	}
	if v, ok := jiraFieldStr(fields, "issuetype"); ok {
		issuetype = v
	}
	description := argString(args, "description", "")
	if v, ok := jiraFieldStr(fields, "description"); ok {
		description = v
	}

	argv := []string{"acli", "jira", "workitem", "create", "-p", project, "-s", summary, "-t", issuetype}
	if description != "" {
		argv = append(argv, "-d", description)
	}
	if assignee := jiraAssignee(args, fields); assignee != "" {
		argv = append(argv, "-a", assignee)
	}
	if v, ok := fields["labels"]; ok {
		argv = append(argv, "-l", jiraJoinLabels(v))
	}

	meta, res, err := jiraRunJSON(ctx, conn, argv...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("jira: create: %s", jiraErrMsg(res))), nil
	}
	return Changed(fmt.Sprintf("created %s issue %q", issuetype, summary)).WithExtra("meta", meta), nil
}

func jiraOpComment(ctx context.Context, conn remoteexec.Connection, args map[string]any, fields map[string]any) (Result, error) {
	issue, err := requireString(args, "issue")
	if err != nil {
		return Result{}, errArg("jira: comment: %v", err)
	}
	comment, err := requireString(args, "comment")
	if err != nil {
		return Result{}, errArg("jira: comment: %v", err)
	}
	if len(fields) > 0 {
		return Result{}, errArg("jira: comment: fields has no acli equivalent for comment create (see this module's own doc comment)")
	}
	if _, ok := args["comment_visibility"]; ok {
		return Fail("jira: comment: comment_visibility has no atomic acli equivalent — acli's comment " +
			"visibility functionality only applies to an already-created comment, not a create-with-" +
			"visibility call; see this module's own doc comment"), nil
	}

	meta, res, err := jiraRunJSON(ctx, conn, "acli", "jira", "workitem", "comment", "create", "-k", issue, "-b", comment)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("jira: comment: %s", jiraErrMsg(res))), nil
	}
	return Changed(fmt.Sprintf("commented on %s", issue)).WithExtra("meta", meta), nil
}

func jiraOpEdit(ctx context.Context, conn remoteexec.Connection, args map[string]any, fields map[string]any) (Result, error) {
	issue, err := requireString(args, "issue")
	if err != nil {
		return Result{}, errArg("jira: edit: %v", err)
	}
	if uerr := jiraUnsupportedFields("edit", fields, "summary", "description", "assignee", "reporter", "labels", "issuetype"); uerr != nil {
		return Result{}, uerr
	}
	if _, ok := fields["reporter"]; ok {
		return Result{}, errArg("jira: edit: fields[reporter] has no acli equivalent (see this module's own doc comment)")
	}

	// Real jira.py's own operation_edit reads ONLY self.vars.fields
	// wholesale ("data = {"fields": self.vars.fields}") — it never
	// touches the top-level summary/description arguments at all for
	// this operation (those are read directly only by operation_create
	// and operation_transition) — so, unlike create, this port reads
	// summary/description from `fields` here too, never from the
	// top-level argument.
	argv := []string{"acli", "jira", "workitem", "edit", "-k", issue, "-y"}
	changedSomething := false
	if v, ok := jiraFieldStr(fields, "summary"); ok && v != "" {
		argv = append(argv, "-s", v)
		changedSomething = true
	}
	if v, ok := jiraFieldStr(fields, "description"); ok && v != "" {
		argv = append(argv, "-d", v)
		changedSomething = true
	}
	if v, ok := fields["issuetype"]; ok {
		argv = append(argv, "-t", fmt.Sprint(v))
		changedSomething = true
	}
	if v, ok := fields["labels"]; ok {
		argv = append(argv, "-l", jiraJoinLabels(v))
		changedSomething = true
	}
	if assignee := jiraAssignee(args, fields); assignee != "" {
		argv = append(argv, "-a", assignee)
		changedSomething = true
	}
	if !changedSomething {
		return Ok("jira: edit: no fields given to change").WithExtra("meta", map[string]any{}), nil
	}

	meta, res, err := jiraRunJSON(ctx, conn, argv...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("jira: edit: %s", jiraErrMsg(res))), nil
	}
	return Changed(fmt.Sprintf("edited %s", issue)).WithExtra("meta", meta), nil
}

func jiraJoinLabels(v any) string {
	switch list := v.(type) {
	case []any:
		out := make([]string, len(list))
		for i, item := range list {
			out[i] = fmt.Sprint(item)
		}
		return strings.Join(out, ",")
	case []string:
		return strings.Join(list, ",")
	default:
		return fmt.Sprint(v)
	}
}

func jiraOpFetch(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	issue, err := requireString(args, "issue")
	if err != nil {
		return Result{}, errArg("jira: fetch: %v", err)
	}
	meta, res, err := jiraRunJSON(ctx, conn, "acli", "jira", "workitem", "view", issue, "-f", "*all")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("jira: fetch: %s", jiraErrMsg(res))), nil
	}
	return Ok(fmt.Sprintf("fetched %s", issue)).WithExtra("meta", meta), nil
}

func jiraOpLink(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	linktype, err := requireString(args, "linktype")
	if err != nil {
		return Result{}, errArg("jira: link: %v", err)
	}
	inward, err := requireString(args, "inwardissue")
	if err != nil {
		return Result{}, errArg("jira: link: %v", err)
	}
	outward, err := requireString(args, "outwardissue")
	if err != nil {
		return Result{}, errArg("jira: link: %v", err)
	}

	meta, res, err := jiraRunJSON(ctx, conn, "acli", "jira", "workitem", "link", "create",
		"--out", outward, "--in", inward, "--type", linktype, "--yes")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("jira: link: %s", jiraErrMsg(res))), nil
	}
	return Changed(fmt.Sprintf("linked %s -> %s (%s)", outward, inward, linktype)).WithExtra("meta", meta), nil
}

func jiraOpSearch(ctx context.Context, conn remoteexec.Connection, args map[string]any, fields map[string]any) (Result, error) {
	jql, err := requireString(args, "jql")
	if err != nil {
		return Result{}, errArg("jira: search: %v", err)
	}
	argv := []string{"acli", "jira", "workitem", "search", "--jql", jql}
	if len(fields) > 0 {
		names := make([]string, 0, len(fields))
		for k := range fields {
			names = append(names, k)
		}
		argv = append(argv, "-f", strings.Join(names, ","))
	}
	if n := argInt(args, "maxresults", 0); n > 0 {
		argv = append(argv, "--limit", fmt.Sprint(n))
	}

	meta, res, err := jiraRunJSON(ctx, conn, argv...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("jira: search: %s", jiraErrMsg(res))), nil
	}
	return Ok("jira: search complete").WithExtra("meta", meta), nil
}

func jiraOpTransition(ctx context.Context, conn remoteexec.Connection, args map[string]any, fields map[string]any) (Result, error) {
	issue, err := requireString(args, "issue")
	if err != nil {
		return Result{}, errArg("jira: transition: %v", err)
	}
	status := argString(args, "status", "")
	statusID := argString(args, "status_id", "")
	if status == "" && statusID == "" {
		return Result{}, errArg("jira: transition: one of status or status_id is required")
	}
	if status == "" && statusID != "" {
		return Fail("jira: transition: status_id has no verified acli equivalent — acli's own transition " +
			"command exposes only --status (documented as a status NAME, with no confirmed support for a " +
			"numeric transition ID), and this port does not guess; see this module's own doc comment"), nil
	}
	if len(fields) > 0 {
		return Result{}, errArg("jira: transition: fields has no acli equivalent for transition (acli's transition command has no field-setting flags); see this module's own doc comment")
	}
	if argString(args, "summary", "") != "" || argString(args, "description", "") != "" {
		return Fail("jira: transition: summary/description cannot be changed as part of a transition through " +
			"acli — its transition command has no such flags; see this module's own doc comment"), nil
	}

	meta, res, err := jiraRunJSON(ctx, conn, "acli", "jira", "workitem", "transition", "-k", issue, "-s", status, "-y")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("jira: transition: %s", jiraErrMsg(res))), nil
	}

	comment := argString(args, "comment", "")
	if comment == "" {
		return Changed(fmt.Sprintf("transitioned %s to %s", issue, status)).WithExtra("meta", meta), nil
	}

	// Best-effort, NON-ATOMIC follow-up: real jira.py adds a transition
	// comment in the SAME REST call; acli has no such flag, so this port
	// issues a second, separate call — see this module's own doc
	// comment. The transition has already succeeded at this point, so a
	// failure here is reported distinctly rather than undoing it.
	commentMeta, cres, cerr := jiraRunJSON(ctx, conn, "acli", "jira", "workitem", "comment", "create", "-k", issue, "-b", comment)
	if cerr != nil {
		return Result{}, cerr
	}
	if cres.RC != 0 {
		return Changed(fmt.Sprintf("transitioned %s to %s, but the follow-up comment failed: %s",
			issue, status, jiraErrMsg(cres))).WithExtra("meta", meta), nil
	}
	return Changed(fmt.Sprintf("transitioned %s to %s and added a comment", issue, status)).
		WithExtra("meta", meta).WithExtra("comment_meta", commentMeta), nil
}
