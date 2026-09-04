package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSlVmCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v slcli": {RC: 0},
		"slcli --format json vs list --hostname instance-1 --domain anydomain.com --datacenter dal09": {
			RC: 0, Stdout: `[]`,
		},
		"slcli --format json vs create --hostname instance-1 --domain anydomain.com --datacenter dal09 --cpu 1 --memory 1024 --billing hourly --os UBUNTU_LATEST --disk 25 --wait 600": {
			RC: 0, Stdout: `{"id":12345,"hostname":"instance-1"}`,
		},
	})
	args := map[string]any{
		"hostname":   "instance-1",
		"domain":     "anydomain.com",
		"datacenter": "dal09",
		"cpus":       1,
		"memory":     1024,
		"disks":      []any{25},
		"os_code":    "UBUNTU_LATEST",
	}
	res, err := moduleSlVm(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want Changed=true")
	}
	inst, ok := res.Extra["instance"].(map[string]any)
	if !ok || inst["hostname"] != "instance-1" {
		t.Fatalf("instance = %+v", res.Extra["instance"])
	}
}

func TestModuleSlVmCreateAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v slcli": {RC: 0},
		"slcli --format json vs list --hostname instance-1 --domain anydomain.com --datacenter dal09": {
			RC: 0, Stdout: `[{"id":999,"hostname":"instance-1"}]`,
		},
	})
	args := map[string]any{
		"hostname":   "instance-1",
		"domain":     "anydomain.com",
		"datacenter": "dal09",
		"os_code":    "UBUNTU_LATEST",
	}
	res, err := moduleSlVm(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSlVmCreateNeitherOsNorImageNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v slcli": {RC: 0},
		"slcli --format json vs list --hostname instance-1 --domain anydomain.com": {
			RC: 0, Stdout: `[]`,
		},
	})
	args := map[string]any{
		"hostname": "instance-1",
		"domain":   "anydomain.com",
	}
	res, err := moduleSlVm(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSlVmCancelByInstanceID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v slcli":                       {RC: 0},
		"slcli --format json -y vs cancel 12345": {RC: 0},
	})
	res, err := moduleSlVm(context.Background(), conn, map[string]any{
		"state":       "absent",
		"instance_id": "12345",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSlVmCancelByInstanceIDSwallowsError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v slcli":                       {RC: 0},
		"slcli --format json -y vs cancel 99999": {RC: 1, Stderr: "not found"},
	})
	res, err := moduleSlVm(context.Background(), conn, map[string]any{
		"state":       "absent",
		"instance_id": "99999",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want Failed=false (real sl_vm swallows cancel errors)", res)
	}
	if res.Changed {
		t.Fatal("want Changed=false on a swallowed cancel error")
	}
}

func TestModuleSlVmCancelByTag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v slcli": {RC: 0},
		"slcli --format json vs list --tag ansible-module-test": {
			RC: 0, Stdout: `[{"id":1},{"id":2}]`,
		},
		"slcli --format json -y vs cancel 1": {RC: 0},
		"slcli --format json -y vs cancel 2": {RC: 0},
	})
	res, err := moduleSlVm(context.Background(), conn, map[string]any{
		"state": "absent",
		"tags":  "ansible-module-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSlVmCancelNoopWhenNothingIdentifies(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v slcli": {RC: 0},
	})
	res, err := moduleSlVm(context.Background(), conn, map[string]any{"state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("expected only the binary check, got %v", conn.Commands)
	}
}
