package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGithubKey implements Ansible's `github_key` (community.general)
// module: creates, removes, or updates an SSH key on the AUTHENTICATED
// USER's own GitHub account (real github_key talks to `/user/keys`,
// not a repository), via `gh ssh-key add`/`list`/`delete` — see
// github_common.go's own doc comment for why this port substitutes the
// `gh` CLI for real github_key's own direct GitHub REST API calls.
//
// Args: token (required) — wired into GH_TOKEN for each `gh`
// invocation (see github_common.go), matching real github_key's own
// required token argument exactly (this module has no username/
// password of its own to accept, unlike github_deploy_key/
// github_release/github_webhook*); name (required) — the key's title;
// pubkey (required when state=present); state (present|absent,
// default present); force (bool, default true) — see below.
//
// Deviation — api_url is accepted for argument-shape compatibility but
// has no effect, same reasoning as github_deploy_key's github_url (see
// that module's own doc comment): `gh` targets whatever host it is
// already authenticated against, not a per-invocation API base URL.
//
// Deviation — scope: `gh ssh-key list`/`add` cover BOTH "authentication"
// and "signing" GitHub SSH key types (its own --type flag, default
// "authentication"); real github_key's own `/user/keys` REST endpoint
// only ever sees authentication keys. This port filters `gh ssh-key
// list`'s own output to type=="authentication" everywhere, and never
// passes --type to `gh ssh-key add` (leaving its own default), so its
// observable scope matches real github_key's exactly.
//
// Idempotency, matching real github_key.py's own ensure_key_present/
// ensure_key_absent exactly: state=present lists every authentication
// key, filters to matching_keys with Title==name, and separately fails
// (Result{Failed:true}, matching real module.fail_json) if `pubkey`'s
// own base64 blob (its second whitespace-separated field) matches a
// DIFFERENT key's blob under a different title — GitHub itself refuses
// a duplicate key across accounts/titles, and real github_key.py
// detects this itself first for a clearer message. If a matching_keys
// entry exists with a DIFFERENT blob and force=true, it is deleted
// first (`gh ssh-key delete <id> -y`) and treated as absent; if no
// matching key remains, a new one is added (`gh ssh-key add -` with
// pubkey piped over stdin, `-t name`) — Changed=true; if a matching key
// with the SAME blob already exists (or survived because force=false),
// Changed=false. state=absent deletes every key with Title==name
// (there is no uniqueness constraint on title enforced client-side,
// matching real github_key.py's own to_delete list-comprehension,
// which does not assume exactly one); Changed reflects whether any
// were found.
//
// Extra["key"] (state=present) and Extra["deleted_keys"]/
// Extra["matching_keys"] mirror real github_key's own return shape,
// each key rendered as {id,key,title} — real github_key's own richer
// per-key object (url/created_at/read_only/verified, from GitHub's
// full REST key resource) is not reproducible from `gh ssh-key
// list`'s own plain tab-separated columns (title, key, created_at,
// id, type — see ghSSHKeyList's own doc comment), a documented gap.
func moduleGithubKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("github_key: state must be one of present, absent, got %q", state)
	}
	force := argBool(args, "force", true)
	pubkey := argString(args, "pubkey", "")
	if pubkey != "" && len(strings.Fields(pubkey)) < 2 {
		return Result{}, errArg("github_key: pubkey has an invalid format")
	}
	if pubkey == "" && state == "present" {
		return Result{}, errArg("github_key: pubkey is required when state=present")
	}

	if state == "absent" {
		return githubKeyEnsureAbsent(ctx, conn, token, name)
	}
	return githubKeyEnsurePresent(ctx, conn, token, name, pubkey, force)
}

type ghSSHKeyEntry struct {
	Title, Key, CreatedAt, ID, Type string
}

func (e ghSSHKeyEntry) toExtra() map[string]any {
	return map[string]any{"id": e.ID, "key": e.Key, "title": e.Title}
}

// ghSSHKeyList runs `gh ssh-key list` and parses its plain
// tab-separated output (title, key, created_at, id, type — `gh ssh-key
// list` has no --json support, verified directly against this batch's
// own locally installed `gh` binary), returning only type=="authentication"
// entries (see moduleGithubKey's own doc comment on scope).
func ghSSHKeyList(ctx context.Context, conn remoteexec.Connection, token string) ([]ghSSHKeyEntry, error) {
	res, err := ghRun(ctx, conn, token, nil, "ssh-key", "list")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	var out []ghSSHKeyEntry
	for _, line := range strings.Split(res.Stdout, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 5 {
			continue
		}
		if f[4] != "authentication" {
			continue
		}
		out = append(out, ghSSHKeyEntry{Title: f[0], Key: f[1], CreatedAt: f[2], ID: f[3], Type: f[4]})
	}
	return out, nil
}

func ghSSHKeySignature(key string) string {
	f := strings.Fields(key)
	if len(f) < 2 {
		return ""
	}
	return f[1]
}

func githubKeyEnsureAbsent(ctx context.Context, conn remoteexec.Connection, token, name string) (Result, error) {
	all, err := ghSSHKeyList(ctx, conn, token)
	if err != nil {
		return Result{}, err
	}
	var toDelete []ghSSHKeyEntry
	for _, k := range all {
		if k.Title == name {
			toDelete = append(toDelete, k)
		}
	}
	deleted := make([]any, 0, len(toDelete))
	for _, k := range toDelete {
		res, err := ghRun(ctx, conn, token, nil, "ssh-key", "delete", k.ID, "-y")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("github_key: failed to delete key " + k.ID + ": " + ghStderr(res)), nil
		}
		deleted = append(deleted, k.toExtra())
	}
	res := Result{Changed: len(toDelete) > 0}
	return res.WithExtra("deleted_keys", deleted), nil
}

func githubKeyEnsurePresent(ctx context.Context, conn remoteexec.Connection, token, name, pubkey string, force bool) (Result, error) {
	all, err := ghSSHKeyList(ctx, conn, token)
	if err != nil {
		return Result{}, err
	}
	newSig := ghSSHKeySignature(pubkey)

	var matching []ghSSHKeyEntry
	for _, k := range all {
		if k.Title == name {
			matching = append(matching, k)
		}
		if ghSSHKeySignature(k.Key) == newSig && k.Title != name {
			return Fail("github_key: another key with the same content is already registered under the name |" + k.Title + "|"), nil
		}
	}

	var deleted []any
	if len(matching) > 0 && force && ghSSHKeySignature(matching[0].Key) != newSig {
		for _, k := range matching {
			res, err := ghRun(ctx, conn, token, nil, "ssh-key", "delete", k.ID, "-y")
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("github_key: failed to delete key " + k.ID + ": " + ghStderr(res)), nil
			}
			deleted = append(deleted, k.toExtra())
		}
		matching = nil
	}

	changed := len(deleted) > 0 || len(matching) == 0
	var newKey map[string]any
	matchingExtra := make([]any, 0, len(matching))
	if len(matching) == 0 {
		addRes, err := ghRun(ctx, conn, token, strings.NewReader(pubkey), "ssh-key", "add", "-", "-t", name)
		if err != nil {
			return Result{}, err
		}
		if addRes.RC != 0 {
			return Fail("github_key: failed to add key: " + ghStderr(addRes)), nil
		}
		// gh ssh-key add prints no machine-readable id; re-list to find
		// the freshly created entry, matching real create_key's own
		// returned key metadata as closely as this port can observe it.
		after, err := ghSSHKeyList(ctx, conn, token)
		if err != nil {
			return Result{}, err
		}
		for _, k := range after {
			if k.Title == name {
				newKey = k.toExtra()
				break
			}
		}
	} else {
		newKey = matching[0].toExtra()
		for _, k := range matching {
			matchingExtra = append(matchingExtra, k.toExtra())
		}
	}

	res := Result{Changed: changed}
	res = res.WithExtra("deleted_keys", deleted)
	res = res.WithExtra("matching_keys", matchingExtra)
	res = res.WithExtra("key", newKey)
	return res, nil
}
