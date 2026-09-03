package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXbpsInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xbps-install -Sy":                {RC: 0},
		"xbps-query curl >/dev/null 2>&1": {RC: 1},
		"xbps-install -y curl":            {RC: 0},
	})
	res, err := moduleXbps(context.Background(), conn, map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if len(conn.Commands) != 3 || conn.Commands[0] != "xbps-install -Sy" {
		t.Fatalf("commands = %v, want update_cache to default true", conn.Commands)
	}
}

func TestModuleXbpsInstallNoUpdateCache(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xbps-query curl >/dev/null 2>&1": {RC: 1},
		"xbps-install -y curl":            {RC: 0},
	})
	res, err := moduleXbps(context.Background(), conn, map[string]any{"name": "curl", "update_cache": false})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleXbpsAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xbps-install -Sy":                {RC: 0},
		"xbps-query curl >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleXbps(context.Background(), conn, map[string]any{"name": "curl", "update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleXbpsAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xbps-install -Sy":                {RC: 0},
		"xbps-query curl >/dev/null 2>&1": {RC: 0},
		"xbps-remove -y curl":             {RC: 0},
	})
	res, err := moduleXbps(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleXbpsLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xbps-install -Sy":      {RC: 0},
		"xbps-install -uy curl": {RC: 0},
	})
	res, err := moduleXbps(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleXbpsUpgrade(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xbps-install -Sy":  {RC: 0},
		"xbps-install -Suy": {RC: 0},
	})
	res, err := moduleXbps(context.Background(), conn, map[string]any{"upgrade": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleXbpsUpgradeAndNameMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXbps(context.Background(), conn, map[string]any{"name": "curl", "upgrade": true}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleXbpsUpdateCacheOnlyNoName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xbps-install -Sy": {RC: 0},
	})
	res, err := moduleXbps(context.Background(), conn, map[string]any{"update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if res.Msg != "nothing to do" {
		t.Fatalf("msg = %q", res.Msg)
	}
}
