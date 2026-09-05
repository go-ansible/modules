package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleEasyInstallAlreadySatisfied(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v easy_install":         {RC: 0},
		"easy_install --dry-run pip 2>&1": {RC: 0, Stdout: "Best match: pip 24.0\nProcessing pip-24.0\n"},
	})
	res, err := moduleEasyInstall(context.Background(), conn, map[string]any{"name": "pip"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleEasyInstallInstalls(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v easy_install":         {RC: 0},
		"easy_install --dry-run pip 2>&1": {RC: 0, Stdout: "Downloading https://pypi.org/pip-24.0.tar.gz\n"},
		"easy_install pip":                {RC: 0},
	})
	res, err := moduleEasyInstall(context.Background(), conn, map[string]any{"name": "pip"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleEasyInstallLatestAddsUpgrade(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v easy_install":                   {RC: 0},
		"easy_install --upgrade --dry-run pip 2>&1": {RC: 0, Stdout: "Downloading https://pypi.org/pip-24.1.tar.gz\n"},
		"easy_install --upgrade pip":                {RC: 0},
	})
	res, err := moduleEasyInstall(context.Background(), conn, map[string]any{"name": "pip", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleEasyInstallMissingBinaryFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v easy_install": {RC: 1},
	})
	_, err := moduleEasyInstall(context.Background(), conn, map[string]any{"name": "pip"})
	if err == nil {
		t.Fatal("want error when easy_install is not on PATH")
	}
}

func TestModuleEasyInstallVirtualenv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/env/bin/activate":                   {RC: 1},
		"virtualenv /opt/env":                             {RC: 0},
		"test -e /opt/env/bin/easy_install":               {RC: 0},
		"/opt/env/bin/easy_install --dry-run bottle 2>&1": {RC: 0, Stdout: "Downloading bottle\n"},
		"/opt/env/bin/easy_install bottle":                {RC: 0},
	})
	res, err := moduleEasyInstall(context.Background(), conn, map[string]any{"name": "bottle", "virtualenv": "/opt/env"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if res.Extra["binary"] != "/opt/env/bin/easy_install" {
		t.Fatalf("binary = %v", res.Extra["binary"])
	}
}
