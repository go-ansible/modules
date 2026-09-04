package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGithubDeployKey implements Ansible's `github_deploy_key`
// (community.general) module: adds or removes a deploy key on a
// GitHub repository, via `gh repo deploy-key add`/`delete`/`list` —
// see github_common.go's own doc comment for why this port substitutes
// the `gh` CLI for real github_deploy_key's own direct GitHub REST API
// calls, and for the auth-argument precondition/mapping (this module's
// own username/password/otp arguments are accepted for
// argument-shape compatibility but have no effect; token, if given, is
// wired into GH_TOKEN for each `gh` invocation).
//
// Args: owner (required, aliases account/organization); repo
// (required, alias repository); name (required, aliases title/label)
// — the deploy key's title; key (required) — the SSH public key text;
// read_only (bool, default true) — omits `gh`'s own `-w/--allow-write`
// flag when true; state (present|absent, default present); force
// (bool, default false) — matches real github_deploy_key's own
// documented force-replace-by-deleting-any-matching-key-first
// semantics (see below); username/password/otp — accepted, no effect
// (see github_common.go); token — wired into GH_TOKEN.
//
// Deviation — github_url is accepted for argument-shape compatibility
// but has no effect: `gh` targets whatever host its own current
// authentication is for (see `gh auth login --hostname`), not a
// per-invocation API base URL; a GitHub Enterprise Server target needs
// `gh` itself already authenticated against that host on the target,
// not a module argument.
//
// Idempotency and force semantics, matching real github_deploy_key.py's
// own GithubDeployKey.get_existing_key() and main() control flow
// exactly: an existing key is looked up via `gh repo deploy-key list`,
// matched either by its PUBLIC KEY CONTENT (the first two
// whitespace-separated fields — algorithm + base64 blob, ignoring any
// trailing comment) equaling `key`'s own, or — only when force=true —
// by its TITLE equaling `name`. If state=absent or force=true, that
// lookup runs first and, if it finds a match, deletes it via `gh repo
// deploy-key delete`; state=absent then reports Changed accordingly
// (true if a key was deleted, false with "Deploy key does not exist"
// if none matched). Then, if state=present, this port always attempts
// `gh repo deploy-key add` (the key content is piped over stdin via
// gh's own "-" key-file argument, never touching a temp file — see
// ghRun's own doc comment): success is a new key (Changed=true); a
// failure re-checks for an existing key by content match — if now
// found (e.g. it already existed and force was false, so the earlier
// force-only lookup never ran), Changed=false ("Deploy key already
// exists"); otherwise this port fails with `gh`'s own error text.
//
// Deviation — failure classification: real github_deploy_key.py
// distinguishes "the add failed because the key already exists" from
// every other failure by the GitHub REST API's own HTTP 422 status
// code (fetch_url's own info["status"]). `gh repo deploy-key add`'s
// text output gives this port no equivalently reliable status-code
// signal to grep for, so this port instead ALWAYS re-checks for an
// existing matching key after ANY add failure and treats "found" as
// the same non-failure outcome real github_deploy_key.py's own 422
// branch does — a strictly safer generalization (it can only turn a
// would-be Fail into a Changed=false success when the key really is
// already there), not a narrower one.
func moduleGithubDeployKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	owner, err := requireString(args, "owner")
	if err != nil {
		return Result{}, err
	}
	repo, err := requireString(args, "repo")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("github_deploy_key: state must be one of present, absent, got %q", state)
	}
	readOnly := argBool(args, "read_only", true)
	force := argBool(args, "force", false)
	token := argString(args, "token", "")
	spec := owner + "/" + repo

	existingID, _, err := ghDeployKeyFind(ctx, conn, token, spec, name, key, force)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" || force {
		if existingID != "" {
			delRes, err := ghRun(ctx, conn, token, nil, "repo", "deploy-key", "delete", existingID, "-R", spec)
			if err != nil {
				return Result{}, err
			}
			if delRes.RC != 0 {
				return Fail("github_deploy_key: failed to delete existing deploy key: " + ghStderr(delRes)), nil
			}
			if state == "absent" {
				id, _ := strconv.Atoi(existingID)
				return Changed("Deploy key successfully deleted").WithExtra("id", id), nil
			}
			existingID = ""
		} else if state == "absent" {
			return Ok("Deploy key does not exist"), nil
		}
	}

	if state != "present" {
		return Ok(""), nil
	}

	addArgs := []string{"repo", "deploy-key", "add", "-", "-t", name, "-R", spec}
	if !readOnly {
		addArgs = append(addArgs, "-w")
	}
	addRes, err := ghRun(ctx, conn, token, strings.NewReader(key), addArgs...)
	if err != nil {
		return Result{}, err
	}
	if addRes.RC == 0 {
		id, _, ferr := ghDeployKeyFind(ctx, conn, token, spec, name, key, force)
		if ferr != nil {
			return Result{}, ferr
		}
		idInt, _ := strconv.Atoi(id)
		return Changed("Deploy key successfully added").WithExtra("id", idInt), nil
	}

	// Add failed — see this function's own doc comment on why any
	// failure re-checks for an existing match (the same force-aware
	// matching rule ghDeployKeyFind always applies) rather than
	// trusting an HTTP-status-only signal this port cannot observe
	// through `gh`.
	recheckID, _, err := ghDeployKeyFind(ctx, conn, token, spec, name, key, force)
	if err != nil {
		return Result{}, err
	}
	if recheckID != "" {
		return Ok("Deploy key already exists"), nil
	}
	return Fail("github_deploy_key: failed to add deploy key: " + ghStderr(addRes)), nil
}

type ghDeployKeyEntry struct {
	ID    int    `json:"id"`
	Key   string `json:"key"`
	Title string `json:"title"`
}

// ghDeployKeyFind lists deploy keys on spec and returns the ID and raw
// key text of the first one matching by content (or, only when force
// is set, by title) — see moduleGithubDeployKey's own doc comment for
// the exact matching rule, mirrored from real GithubDeployKey.get_existing_key().
func ghDeployKeyFind(ctx context.Context, conn remoteexec.Connection, token, spec, name, key string, force bool) (id, keyText string, err error) {
	var entries []ghDeployKeyEntry
	res, err := ghRunJSON(ctx, conn, token, &entries, "repo", "deploy-key", "list", "--json", "id,key,title", "-R", spec)
	if err != nil {
		return "", "", err
	}
	if res.RC != 0 {
		return "", "", nil
	}
	for _, e := range entries {
		if ghDeployKeySameContent(e.Key, key) {
			return strconv.Itoa(e.ID), e.Key, nil
		}
		if force && e.Title == name {
			return strconv.Itoa(e.ID), e.Key, nil
		}
	}
	return "", "", nil
}

// ghDeployKeySameContent compares two SSH public keys by their first
// two whitespace-separated fields (algorithm + base64 blob), ignoring
// any trailing comment — matching real GithubDeployKey's own
// `i["key"].split() == self.key.split()[:2]`.
func ghDeployKeySameContent(a, b string) bool {
	af, bf := strings.Fields(a), strings.Fields(b)
	if len(af) < 2 || len(bf) < 2 {
		return false
	}
	return af[0] == bf[0] && af[1] == bf[1]
}
