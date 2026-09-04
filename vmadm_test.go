package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const vmadmTestUUID = "564d9e32-4d33-8c1b-6c8b-f5e0f5e0f5e0"

func TestModuleVmadmMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleVmadm(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when neither name nor uuid is given")
	}
}

func TestModuleVmadmInvalidUUID(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleVmadm(context.Background(), conn, map[string]any{"uuid": "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVmadmAlreadyRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vmadm lookup -j -o state uuid=" + vmadmTestUUID: {RC: 0, Stdout: `[{"state":"running"}]`},
	})
	res, err := moduleVmadm(context.Background(), conn, map[string]any{"uuid": vmadmTestUUID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVmadmTransitionToRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vmadm lookup -j -o state uuid=" + vmadmTestUUID: {RC: 0, Stdout: `[{"state":"stopped"}]`},
		"vmadm start " + vmadmTestUUID:                   {RC: 0, Stderr: "Successfully started VM " + vmadmTestUUID + "\n"},
	})
	res, err := moduleVmadm(context.Background(), conn, map[string]any{"uuid": vmadmTestUUID, "state": "running"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVmadmCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vmadm lookup -j -o uuid alias=newvm":                                             {RC: 0, Stdout: "[]"},
		"vmadm lookup -j -o state uuid=":                                                  {RC: 0, Stdout: "[]"},
		"umask 077 && cat > /tmp/vmadm-payload.json && chmod 400 /tmp/vmadm-payload.json": {RC: 0},
		"vmadm create -f /tmp/vmadm-payload.json":                                         {RC: 0, Stderr: "Successfully created VM " + vmadmTestUUID + "\n"},
	})
	res, err := moduleVmadm(context.Background(), conn, map[string]any{
		"name": "newvm", "brand": "joyent", "ram": 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["uuid"] != vmadmTestUUID {
		t.Fatalf("uuid = %v", res.Extra["uuid"])
	}
	// The payload was piped over stdin, never touching argv/the command
	// string — confirm it actually carries the requested properties.
	found := false
	for _, s := range conn.Stdins {
		if s != "" {
			found = true
			if !strings.Contains(s, `"brand":"joyent"`) || !strings.Contains(s, `"name":"newvm"`) {
				t.Fatalf("payload stdin = %q", s)
			}
		}
	}
	if !found {
		t.Fatal("want a non-empty payload written to the target's stdin")
	}
}

func TestModuleVmadmDeleteAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vmadm lookup -j -o uuid alias=gone": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleVmadm(context.Background(), conn, map[string]any{"name": "gone", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVmadmWildcardCreatedRejected(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleVmadm(context.Background(), conn, map[string]any{"uuid": "*", "state": "created"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVmadmWildcardManageAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"vmadm lookup -j -o uuid":                        {RC: 0, Stdout: `[{"uuid":"` + vmadmTestUUID + `"}]`},
		"vmadm lookup -j -o state uuid=" + vmadmTestUUID: {RC: 0, Stdout: `[{"state":"stopped"}]`},
		"vmadm start " + vmadmTestUUID:                   {RC: 0, Stderr: "Successfully started VM " + vmadmTestUUID + "\n"},
	})
	res, err := moduleVmadm(context.Background(), conn, map[string]any{"uuid": "*", "state": "running"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
