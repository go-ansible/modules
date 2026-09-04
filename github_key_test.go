package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const ghSSHKeyListOutOne = "mykey\tssh-ed25519 AAAAC3\t2024-01-01T00:00:00Z\t42\tauthentication\n"
const ghSSHKeyListOutTwo = "mykey\tssh-ed25519 AAAAC3\t2024-01-01T00:00:00Z\t42\tauthentication\nother\tssh-ed25519 BBBBC3\t2024-01-01T00:00:00Z\t43\tauthentication\n"

func TestModuleGithubKeyAddNew(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"GH_TOKEN=t gh ssh-key list":           {{RC: 0, Stdout: ""}, {RC: 0, Stdout: ghSSHKeyListOutOne}},
		"GH_TOKEN=t gh ssh-key add - -t mykey": {{RC: 0}},
	})
	res, err := moduleGithubKey(context.Background(), conn, map[string]any{
		"token": "t", "name": "mykey", "pubkey": "ssh-ed25519 AAAAC3 comment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	key, ok := res.Extra["key"].(map[string]any)
	if !ok || key["id"] != "42" {
		t.Fatalf("key = %+v", res.Extra["key"])
	}
}

func TestModuleGithubKeyAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh ssh-key list": {RC: 0, Stdout: ghSSHKeyListOutOne},
	})
	res, err := moduleGithubKey(context.Background(), conn, map[string]any{
		"token": "t", "name": "mykey", "pubkey": "ssh-ed25519 AAAAC3 comment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubKeyForceReplace(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"GH_TOKEN=t gh ssh-key list":           {{RC: 0, Stdout: ghSSHKeyListOutOne}, {RC: 0, Stdout: ghSSHKeyListOutOne}},
		"GH_TOKEN=t gh ssh-key delete 42 -y":   {{RC: 0}},
		"GH_TOKEN=t gh ssh-key add - -t mykey": {{RC: 0}},
	})
	res, err := moduleGithubKey(context.Background(), conn, map[string]any{
		"token": "t", "name": "mykey", "pubkey": "ssh-ed25519 ZZZZ different", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	deleted, _ := res.Extra["deleted_keys"].([]any)
	if len(deleted) != 1 {
		t.Fatalf("deleted_keys = %+v", res.Extra["deleted_keys"])
	}
}

func TestModuleGithubKeyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh ssh-key list":         {RC: 0, Stdout: ghSSHKeyListOutTwo},
		"GH_TOKEN=t gh ssh-key delete 42 -y": {RC: 0},
	})
	res, err := moduleGithubKey(context.Background(), conn, map[string]any{
		"token": "t", "name": "mykey", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubKeyAbsentNoOp(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh ssh-key list": {RC: 0, Stdout: ""},
	})
	res, err := moduleGithubKey(context.Background(), conn, map[string]any{
		"token": "t", "name": "mykey", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubKeyMissingPubkey(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGithubKey(context.Background(), conn, map[string]any{"token": "t", "name": "mykey"}); err == nil {
		t.Fatal("want error for missing pubkey when state=present")
	}
}
