package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleBeadmCreateSolarish(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":           {RC: 0, Stdout: "SunOS\n"},
		"beadm list -H":      {RC: 0, Stdout: "oldbe;NR;/;164512;static;2020-01-01 00:00\n"},
		"beadm create newbe": {RC: 0},
	})
	res, err := moduleBeadm(context.Background(), conn, map[string]any{"name": "newbe"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleBeadmCreateAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":      {RC: 0, Stdout: "SunOS\n"},
		"beadm list -H": {RC: 0, Stdout: "newbe;NR;/;164512;static;2020-01-01 00:00\n"},
	})
	res, err := moduleBeadm(context.Background(), conn, map[string]any{"name": "newbe"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleBeadmDestroyMountedRefused(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":      {RC: 0, Stdout: "SunOS\n"},
		"beadm list -H": {RC: 0, Stdout: "oldbe;uuid;-;/mnt/be;164512;static;2020-01-01 00:00\n"},
	})
	res, err := moduleBeadm(context.Background(), conn, map[string]any{"name": "oldbe", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: mounted BE cannot be destroyed")
	}
}

func TestModuleBeadmDestroy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":               {RC: 0, Stdout: "SunOS\n"},
		"beadm list -H":          {RC: 0, Stdout: "oldbe;uuid;-;;164512;static;2020-01-01 00:00\n"},
		"beadm destroy -F oldbe": {RC: 0},
	})
	res, err := moduleBeadm(context.Background(), conn, map[string]any{"name": "oldbe", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleBeadmActivate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":             {RC: 0, Stdout: "SunOS\n"},
		"beadm list -H":        {RC: 0, Stdout: "newbe;uuid;-;;164512;static;2020-01-01 00:00\n"},
		"beadm activate newbe": {RC: 0},
	})
	res, err := moduleBeadm(context.Background(), conn, map[string]any{"name": "newbe", "state": "activated"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleBeadmMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleBeadm(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
