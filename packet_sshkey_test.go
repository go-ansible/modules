package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacketSshkeyCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v metal":          {RC: 0},
		"metal ssh-key get -o json": {RC: 0, Stdout: `{"ssh_keys":[]}`},
		"metal ssh-key create -k 'ssh-rsa AAAA user@host' -l user@host -o json": {
			RC: 0, Stdout: `{"id":"key-1","label":"user@host","key":"ssh-rsa AAAA user@host"}`,
		},
	})
	args := map[string]any{"key": "ssh-rsa AAAA user@host"}
	res, err := modulePacketSshkey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePacketSshkeyDeleteByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v metal":                         {RC: 0},
		"metal ssh-key get -o json":                {RC: 0, Stdout: `{"ssh_keys":[{"id":"key-1","label":"x"}]}`},
		"metal ssh-key delete -i key-1 -f -o json": {RC: 0},
	})
	args := map[string]any{"id": "key-1", "state": "absent"}
	res, err := modulePacketSshkey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
