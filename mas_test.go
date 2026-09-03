package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMasBinaryMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas": {RC: 1},
	})
	res, err := moduleMas(context.Background(), conn, map[string]any{"id": 409183694})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: mas binary missing")
	}
}

const masListOutput = "409183694  Keynote  (13.1)\n413857545  Divvy  (1.5.5)\n"
const masOutdatedOutput = "409183694  Keynote  (13.0 -> 13.1)\n"

func TestModuleMasInstallNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas":       {RC: 0},
		"mas list 2>/dev/null": {RC: 0, Stdout: masListOutput},
		"mas install 123":      {RC: 0},
	})
	res, err := moduleMas(context.Background(), conn, map[string]any{"id": 123})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleMasPresentAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas":       {RC: 0},
		"mas list 2>/dev/null": {RC: 0, Stdout: masListOutput},
	})
	res, err := moduleMas(context.Background(), conn, map[string]any{"id": 409183694})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleMasLatestUpgradesOutdated(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas":           {RC: 0},
		"mas list 2>/dev/null":     {RC: 0, Stdout: masListOutput},
		"mas outdated 2>/dev/null": {RC: 0, Stdout: masOutdatedOutput},
		"mas upgrade 409183694":    {RC: 0},
	})
	res, err := moduleMas(context.Background(), conn, map[string]any{"id": 409183694, "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleMasLatestUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas":           {RC: 0},
		"mas list 2>/dev/null":     {RC: 0, Stdout: masListOutput},
		"mas outdated 2>/dev/null": {RC: 0, Stdout: ""},
	})
	res, err := moduleMas(context.Background(), conn, map[string]any{"id": 409183694, "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleMasAbsentUninstalls(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas":          {RC: 0},
		"mas list 2>/dev/null":    {RC: 0, Stdout: masListOutput},
		"mas uninstall 413857545": {RC: 0},
	})
	res, err := moduleMas(context.Background(), conn, map[string]any{"id": 413857545, "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleMasUpgradeAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas":           {RC: 0},
		"mas outdated 2>/dev/null": {RC: 0, Stdout: masOutdatedOutput},
		"mas upgrade":              {RC: 0},
	})
	res, err := moduleMas(context.Background(), conn, map[string]any{"upgrade_all": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleMasUpgradeAllNothingOutdated(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas":           {RC: 0},
		"mas outdated 2>/dev/null": {RC: 0, Stdout: ""},
	})
	res, err := moduleMas(context.Background(), conn, map[string]any{"upgrade_all": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleMasInvalidState(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v mas": {RC: 0},
	})
	if _, err := moduleMas(context.Background(), conn, map[string]any{"id": 1, "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
