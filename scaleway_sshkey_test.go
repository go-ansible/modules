package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewaySSHKeyCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw":               {RC: 0},
		"scw iam ssh-key list -o json": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewaySSHKey(context.Background(), conn, map[string]any{
		"ssh_pub_key": "ssh-rsa AAAAtest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 3 {
		t.Fatalf("expected probe+list+create, got %v", conn.Commands)
	}
}

func TestModuleScalewaySSHKeyAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw iam ssh-key list -o json": {
			RC: 0, Stdout: `[{"id":"key1","public_key":"ssh-rsa AAAAtest"}]`,
		},
	})
	res, err := moduleScalewaySSHKey(context.Background(), conn, map[string]any{
		"ssh_pub_key": "ssh-rsa AAAAtest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewaySSHKeyDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw iam ssh-key list -o json": {
			RC: 0, Stdout: `[{"id":"key1","public_key":"ssh-rsa AAAAtest"}]`,
		},
		"scw iam ssh-key delete ssh-key-id=key1": {RC: 0},
	})
	res, err := moduleScalewaySSHKey(context.Background(), conn, map[string]any{
		"ssh_pub_key": "ssh-rsa AAAAtest", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewaySSHKeyDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw":               {RC: 0},
		"scw iam ssh-key list -o json": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleScalewaySSHKey(context.Background(), conn, map[string]any{
		"ssh_pub_key": "ssh-rsa AAAAtest", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
