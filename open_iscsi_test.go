package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOpenIscsiDiscover(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"iscsiadm --mode node": {RC: 21},
		"iscsiadm --mode discovery --type sendtargets --portal sun.com:3260": {RC: 0},
	})
	res, err := moduleOpenIscsi(context.Background(), conn, map[string]any{
		"portal": "sun.com", "discover": true, "show_nodes": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if _, ok := res.Extra["nodes"]; !ok {
		t.Fatal("want nodes in Extra")
	}
}

func TestModuleOpenIscsiDiscoverRequiresPortal(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOpenIscsi(context.Background(), conn, map[string]any{"discover": true}); err == nil {
		t.Fatal("want error for discover without portal")
	}
}

func TestModuleOpenIscsiLoginNewTarget(t *testing.T) {
	target := "iqn.1986-03.com.sun:02:f8c1f9e0"
	conn := newFakeConn(map[string]remoteexec.Result{
		"iscsiadm --mode node":                                     {RC: 0, Stdout: "10.1.1.1:3260,1 " + target + "\n"},
		"iscsiadm --mode session":                                  {RC: 21},
		"iscsiadm --mode node --targetname " + target + " --login": {RC: 0},
	})
	res, err := moduleOpenIscsi(context.Background(), conn, map[string]any{
		"target": target, "login": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleOpenIscsiLoginTargetNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"iscsiadm --mode node": {RC: 21},
	})
	res, err := moduleOpenIscsi(context.Background(), conn, map[string]any{
		"target": "iqn.bogus", "login": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a target not in the cache")
	}
}

func TestModuleOpenIscsiSetAutomatic(t *testing.T) {
	target := "iqn.1986-03.com.sun:02:f8c1f9e0"
	conn := newFakeConn(map[string]remoteexec.Result{
		"iscsiadm --mode node":                        {RC: 21},
		"iscsiadm --mode node --targetname " + target: {RC: 0, Stdout: "node.startup = manual\n"},
		"iscsiadm --mode node --targetname " + target + " --op=update --name node.startup --value automatic": {RC: 0},
	})
	res, err := moduleOpenIscsi(context.Background(), conn, map[string]any{
		"target": target, "auto_node_startup": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleOpenIscsiAutoNodeStartupRequiresTarget(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOpenIscsi(context.Background(), conn, map[string]any{"auto_node_startup": true}); err == nil {
		t.Fatal("want error for auto_node_startup without target")
	}
}
