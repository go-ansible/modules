package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIdracRedfishConfigSetManagerAttributes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v racadm": {RC: 0},
		"racadm set idrac.NTPConfigGroup.1.NTPEnable Enabled": {RC: 0},
		"racadm set idrac.Time.1.Timezone CST6CDT":            {RC: 0},
	})
	args := map[string]any{
		"category": "Manager", "command": []any{"SetManagerAttributes"},
		"resource_id": "iDRAC.Embedded.1", "baseuri": "x", "username": "u", "password": "p",
		"manager_attributes": map[string]any{
			"NTPConfigGroup.1.NTPEnable": "Enabled",
			"Time.1.Timezone":            "CST6CDT",
		},
	}
	res, err := moduleIdracRedfishConfig(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIdracRedfishConfigEmptyAttributes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v racadm": {RC: 0},
	})
	args := map[string]any{
		"category": "Manager", "command": []any{"SetSystemAttributes"}, "baseuri": "x",
		"manager_attributes": map[string]any{},
	}
	res, err := moduleIdracRedfishConfig(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIdracRedfishConfigMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	args := map[string]any{
		"category": "Manager",
		"command":  []any{"SetManagerAttributes", "SetSystemAttributes"},
		"baseuri":  "x",
	}
	res, err := moduleIdracRedfishConfig(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
