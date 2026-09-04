package modules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewaySSHKey implements Ansible's `scaleway_sshkey`
// (community.general) module: adds/removes a public SSH key on a
// Scaleway account, via `scw iam ssh-key create/list/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI.
//
// Args: state (present|absent, default present); ssh_pub_key
// (required).
//
// Deviation — resource substitution: real scaleway_sshkey manages one
// entry inside the CURRENTLY AUTHENTICATED USER's own embedded
// `ssh_public_keys` array (the legacy account.scaleway.com `GET/PATCH
// /users/<id>` resource, a list of bare {"key": "..."} objects with no
// identity of their own beyond the key string) — verified directly
// against scaleway_sshkey.py's own source. `scw`'s own published CLI
// reference has no command for that legacy user-embedded array at all;
// the closest present-day equivalent is `scw iam ssh-key`, a genuinely
// different, newer IAM resource: each SSH key is its OWN
// {id, name, public-key, project-id, ...} object (verified against
// scaleway-cli's own docs/commands/iam.md), not nested inside a user.
// This port uses that substitute, matching by the ssh_pub_key STRING
// content only (never by name, since the legacy resource has none) —
// an honest, disclosed resource-model change, not a silent
// reinterpretation.
//
// Deviation — key naming: `scw iam ssh-key create` requires a `name=`
// argument (verified) that real scaleway_sshkey's own argument_spec has
// no equivalent for at all (the legacy resource is unnamed). This port
// derives a deterministic name "ansible-<8 hex chars of sha256(ssh_pub_key)>"
// so that re-running the same task twice (was it already created?)
// produces the same name — matching does NOT rely on this name, only on
// the public-key content, so the name choice does not affect
// idempotency.
//
// present: `scw iam ssh-key list -o json`, match an entry whose own
// "public-key" (or, defensively, "public_key"/"PublicKey" — see
// scwSSHKeyPublicKey) field, trimmed, equals ssh_pub_key trimmed. Found
// -> Changed=false. Not found -> `scw iam ssh-key create
// name=<derived> public-key=<ssh_pub_key>`, Changed=true.
//
// absent: match found -> `scw iam ssh-key delete
// ssh-key-id=<id>`, Changed=true; not found -> Changed=false.
//
// Extra["sshkey"]: the matched/created key object, decoded directly
// from `scw`'s own JSON output (see scaleway_common.go's own "Output
// shape" caveat).
func moduleScalewaySSHKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_sshkey"); !ok {
		return res, nil
	}
	pubKey, err := requireString(args, "ssh_pub_key")
	if err != nil {
		return Result{}, err
	}
	pubKey = strings.TrimSpace(pubKey)
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_sshkey: state must be present or absent, got %q", state)
	}

	listRes, err := scwRunJSON(ctx, conn, "iam", "ssh-key", "list")
	if err != nil {
		return Result{}, err
	}
	if listRes.RC != 0 {
		return Fail("scaleway_sshkey: failed to list SSH keys: " + scwErrMsg(listRes)), nil
	}
	var keys []map[string]any
	if derr := scwDecode(listRes.Stdout, &keys); derr != nil {
		return Result{}, derr
	}
	var match map[string]any
	for _, k := range keys {
		if scwSSHKeyPublicKey(k) == pubKey {
			match = k
			break
		}
	}

	if state == "absent" {
		if match == nil {
			return Ok(""), nil
		}
		id := scwSSHKeyStr(match, "id")
		delRes, err := scwRun(ctx, conn, "iam", "ssh-key", "delete", "ssh-key-id="+id)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_sshkey: failed to delete SSH key " + id + ": " + scwErrMsg(delRes)), nil
		}
		return Changed(""), nil
	}

	if match != nil {
		res := Result{Changed: false}
		return res.WithExtra("sshkey", match), nil
	}

	sum := sha256.Sum256([]byte(pubKey))
	name := "ansible-" + hex.EncodeToString(sum[:])[:8]
	createRes, err := scwRunJSON(ctx, conn, "iam", "ssh-key", "create", "name="+name, "public-key="+pubKey)
	if err != nil {
		return Result{}, err
	}
	if createRes.RC != 0 {
		return Fail("scaleway_sshkey: failed to create SSH key: " + scwErrMsg(createRes)), nil
	}
	var created map[string]any
	if derr := scwDecode(createRes.Stdout, &created); derr != nil {
		return Result{}, derr
	}
	return Changed("").WithExtra("sshkey", created), nil
}

func scwSSHKeyStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// scwSSHKeyPublicKey reads a decoded IAM ssh-key object's public-key
// field, tolerating several possible key spellings (see
// scaleway_common.go's own "Output shape" caveat on why this port
// cannot assume one exact JSON schema without a live `scw` binary to
// check against).
func scwSSHKeyPublicKey(m map[string]any) string {
	for _, key := range []string{"public_key", "public-key", "PublicKey", "publicKey"} {
		if s := scwSSHKeyStr(m, key); s != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
