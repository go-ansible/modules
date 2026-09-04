package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacketDeviceCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v metal":                   {RC: 0},
		"metal device get -p proj-1 -o json": {RC: 0, Stdout: `{"devices":[]}`},
		"metal device create -p proj-1 -H my-host -O ubuntu_20_04 -P baremetal_0 -o json": {
			RC: 0, Stdout: `{"id":"dev-1","hostname":"my-host","state":"provisioning"}`,
		},
	})
	args := map[string]any{
		"project_id": "proj-1", "hostnames": []any{"my-host"},
		"operating_system": "ubuntu_20_04", "plan": "baremetal_0",
	}
	res, err := modulePacketDevice(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "dev-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModulePacketDeviceAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v metal":                   {RC: 0},
		"metal device get -p proj-1 -o json": {RC: 0, Stdout: `{"devices":[]}`},
	})
	args := map[string]any{
		"project_id": "proj-1", "hostnames": []any{"my-host"}, "state": "absent",
	}
	res, err := modulePacketDevice(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePacketDeviceMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"command -v metal": {RC: 1}})
	args := map[string]any{"project_id": "proj-1", "hostnames": []any{"my-host"}}
	res, err := modulePacketDevice(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: metal missing")
	}
}
