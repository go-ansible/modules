package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGithubRelease implements Ansible's `github_release`
// (community.general) module: fetches the latest release tag of a
// GitHub repository, or creates a new release, via `gh release view`/
// `gh release create` — see github_common.go's own doc comment for why
// this port substitutes the `gh` CLI for real github_release's own
// github3.py-based REST API calls.
//
// Args: user (required) — the repository owner; repo (required);
// action (required: latest_release|create_release); token — wired
// into GH_TOKEN (see github_common.go); password — accepted, no
// effect (see github_common.go; real github_release's own
// github3.login(user, password=...) basic-auth flow has no `gh`
// equivalent); tag (required for action=create_release); target;
// name; body; draft (bool, default false); prerelease (bool, default
// false).
//
// action=latest_release: `gh release view --json tagName` with no tag
// argument (gh's own documented "no tag: show the latest release"
// behavior, matching real repository.latest_release()). If the
// repository has no releases at all, `gh release view` exits non-zero
// — this port treats that the same as real github_release's own
// `if release: ... else: exit_json(tag=None)`, returning Extra["tag"]
// = nil rather than failing.
//
// action=create_release: `gh release view <tag> --json tagName` first
// checks whether a release for `tag` already exists — if so,
// Changed=false with msg "Release for tag <tag> already exists."
// (Extra["tag"] intentionally omitted on this path, matching real
// github_release.py's own `module.exit_json(changed=False, msg=...)`
// call, which does NOT include a `tag` key here despite the module's
// own RETURN block claiming `tag` is "returned: success" — a real,
// verified inconsistency in the upstream module, preserved rather
// than "fixed" here). Otherwise, creates the release via `gh release
// create <tag> --target <target> --title <name> --notes <body>
// [--draft] [--prerelease]`; Extra["tag"] = tag on success.
//
// Deviation — release notes: `gh release create` refuses to run
// non-interactively without one of --notes/--notes-file/
// --generate-notes/--notes-from-tag (it would otherwise prompt on a
// terminal this port's target session does not have); real
// github_release's own `body` argument is optional (defaults to no
// description at all). This port always passes `--notes` with body's
// own value (empty string when body is unset), which produces a
// release with an empty description rather than a genuinely absent
// one — a narrow, documented gap forced by `gh`'s own non-interactive
// requirements, not a behavioral choice.
func moduleGithubRelease(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	user, err := requireString(args, "user")
	if err != nil {
		return Result{}, err
	}
	repo, err := requireString(args, "repo")
	if err != nil {
		return Result{}, err
	}
	action, err := requireString(args, "action")
	if err != nil {
		return Result{}, err
	}
	if action != "latest_release" && action != "create_release" {
		return Result{}, errArg("github_release: action must be one of latest_release, create_release, got %q", action)
	}
	token := argString(args, "token", "")
	spec := user + "/" + repo

	switch action {
	case "latest_release":
		var v struct {
			TagName string `json:"tagName"`
		}
		res, err := ghRunJSON(ctx, conn, token, &v, "release", "view", "--json", "tagName", "-R", spec)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Ok("").WithExtra("tag", nil), nil
		}
		return Ok("").WithExtra("tag", v.TagName), nil

	case "create_release":
		tag, err := requireString(args, "tag")
		if err != nil {
			return Result{}, err
		}
		checkRes, err := ghRun(ctx, conn, token, nil, "release", "view", tag, "--json", "tagName", "-R", spec)
		if err != nil {
			return Result{}, err
		}
		if checkRes.RC == 0 {
			return Ok("Release for tag " + tag + " already exists."), nil
		}

		createArgs := []string{"release", "create", tag, "-R", spec, "--notes", argString(args, "body", "")}
		if target := argString(args, "target", ""); target != "" {
			createArgs = append(createArgs, "--target", target)
		}
		if name := argString(args, "name", ""); name != "" {
			createArgs = append(createArgs, "--title", name)
		}
		if argBool(args, "draft", false) {
			createArgs = append(createArgs, "--draft")
		}
		if argBool(args, "prerelease", false) {
			createArgs = append(createArgs, "--prerelease")
		}
		createRes, err := ghRun(ctx, conn, token, nil, createArgs...)
		if err != nil {
			return Result{}, err
		}
		if createRes.RC != 0 {
			return Fail("github_release: failed to create release: " + ghStderr(createRes)), nil
		}
		return Changed("").WithExtra("tag", tag), nil
	}
	return Result{}, errArg("github_release: unreachable")
}
