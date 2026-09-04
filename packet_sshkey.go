package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacketSshkey implements Ansible's `packet_sshkey`
// (community.general) module: creates or deletes an SSH key registered
// on Equinix Metal — see packet_common.go's own doc comment for the
// `metal` CLI substitution shared by every packet_* module in this
// batch. Commands used: `ssh-key get` (list, when id is not given —
// confirmed real: "Lists all SSH keys" per metal-cli's own generated
// docs), `ssh-key create -k <key> -l <label>`, `ssh-key delete -i <id>
// -f` — every flag independently confirmed from metal-cli's own
// generated per-command docs.
//
// Args: auth_token; fingerprint (used as a lookup selector — matched
// client-side against each listed key's own `fingerprint` field);
// id (takes precedence for lookup); key (the public key string,
// required to create; also used to read a label from when label is
// not given, matching real packet_sshkey.py's own documented "If you
// keep it empty, it is read from key string" behavior for label —
// this port takes the key's own trailing comment field, the third
// whitespace-separated token, the same convention OpenSSH's own
// authorized_keys format uses); key_file (path — read from the
// TARGET's own filesystem via `cat`, a deliberate deviation from real
// packet_sshkey.py's own control-node-local file read, matching this
// port's own "reach the target only through Connection primitives"
// architecture); label (aliases: name); state (present|absent, default
// present).
//
// Extra["sshkeys"]: a one-element list ({"id", "label", "key",
// "fingerprint"}) matching real packet_sshkey.py's own documented
// `sshkeys` return shape (a list, even though this port — like real
// packet_sshkey.py itself — only ever acts on one key per invocation).
func modulePacketSshkey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := metalRequireBinary(ctx, conn, "packet_sshkey"); !ok {
		return res, nil
	}
	authToken := argString(args, "auth_token", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("packet_sshkey: state must be one of present, absent, got %q", state)
	}
	id := argString(args, "id", "")
	fingerprint := argString(args, "fingerprint", "")
	key := argString(args, "key", "")
	if key == "" {
		if kf := argString(args, "key_file", ""); kf != "" {
			out, err := run(ctx, conn, "cat "+shellQuote(kf))
			if err != nil {
				return Result{}, err
			}
			key = out
		}
	}
	label := argString(args, "label", "")
	if label == "" {
		fields := strings.Fields(key)
		if len(fields) >= 3 {
			label = fields[2]
		}
	}

	var listResp map[string]any
	lres, err := metalRunJSON(ctx, conn, authToken, &listResp, "ssh-key", "get")
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return metalFail("packet_sshkey", "listing ssh keys", lres), nil
	}
	items := metalListArray(listResp)
	var match map[string]any
	var found, ambiguous bool
	switch {
	case id != "":
		match, found, ambiguous = metalFindByField(items, "id", id)
	case fingerprint != "":
		match, found, ambiguous = metalFindByField(items, "fingerprint", fingerprint)
	case key != "":
		match, found, ambiguous = metalFindByField(items, "key", key)
	}
	if ambiguous {
		return Fail("packet_sshkey: more than one ssh key matches; execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("packet_sshkey: already absent"), nil
		}
		kid := fmt.Sprint(match["id"])
		dres, err := metalRun(ctx, conn, authToken, "ssh-key", "delete", "-i", kid, "-f")
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return metalFail("packet_sshkey", "deleting "+kid, dres), nil
		}
		return Changed("packet_sshkey: " + kid + " deleted"), nil
	}

	if found {
		return Ok("packet_sshkey: already present").WithExtra("sshkeys", []map[string]any{match}), nil
	}
	if key == "" {
		return Fail("packet_sshkey: key or key_file is required to create an ssh key"), nil
	}

	var created map[string]any
	cres, err := metalRunJSON(ctx, conn, authToken, &created, "ssh-key", "create", "-k", key, "-l", label)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return metalFail("packet_sshkey", "creating ssh key", cres), nil
	}
	return Changed("packet_sshkey: created").WithExtra("sshkeys", []map[string]any{created}), nil
}
