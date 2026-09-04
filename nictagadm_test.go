package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNictagadmCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nictagadm exists storage0":                                      {RC: 1},
		"nictagadm -v add -p mtu=9000 -p mac=00:1b:21:a3:f5:4d storage0": {RC: 0},
	})
	res, err := moduleNictagadm(context.Background(), conn, map[string]any{
		"name": "storage0", "mac": "00:1b:21:a3:f5:4d", "mtu": 9000, "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleNictagadmCreateAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nictagadm exists storage0": {RC: 0},
	})
	res, err := moduleNictagadm(context.Background(), conn, map[string]any{
		"name": "storage0", "mac": "00:1b:21:a3:f5:4d",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleNictagadmRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nictagadm exists storage0":    {RC: 0},
		"nictagadm -v delete storage0": {RC: 0},
	})
	res, err := moduleNictagadm(context.Background(), conn, map[string]any{
		"name": "storage0", "mac": "00:1b:21:a3:f5:4d", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNictagadmRemoveForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nictagadm exists storage0":       {RC: 0},
		"nictagadm -v delete -f storage0": {RC: 0},
	})
	res, err := moduleNictagadm(context.Background(), conn, map[string]any{
		"name": "storage0", "mac": "00:1b:21:a3:f5:4d", "state": "absent", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNictagadmEtherstub(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"nictagadm exists stub0":    {RC: 1},
		"nictagadm -v add -l stub0": {RC: 0},
	})
	res, err := moduleNictagadm(context.Background(), conn, map[string]any{
		"name": "stub0", "etherstub": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleNictagadmInvalidMAC(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleNictagadm(context.Background(), conn, map[string]any{
		"name": "storage0", "mac": "not-a-mac",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an invalid MAC")
	}
}

func TestModuleNictagadmEtherstubMacMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleNictagadm(context.Background(), conn, map[string]any{
		"name": "storage0", "mac": "00:1b:21:a3:f5:4d", "etherstub": true,
	}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleNictagadmMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleNictagadm(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
