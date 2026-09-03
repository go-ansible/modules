package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAndroidSdkInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sdkmanager --list_installed --channel=0": {
			RC:     0,
			Stdout: "Installed packages:\n  Path | Version | Description | Location\n  -------|---------|-------|-------\n  platform-tools | 27.0.0 | Android SDK Platform-Tools | platform-tools\n",
		},
		"sdkmanager --install 'build-tools;34.0.0' --channel=0": {RC: 0},
	})
	res, err := moduleAndroidSdk(context.Background(), conn, map[string]any{
		"name": "build-tools;34.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Stdins) != 2 || conn.Stdins[1] != "N\n" {
		t.Fatalf("stdins = %v, want license prompt answered N", conn.Stdins)
	}
}

func TestModuleAndroidSdkAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sdkmanager --list_installed --channel=0": {
			RC:     0,
			Stdout: "Installed packages:\n  Path | Version | Description | Location\n  -------|---------|-------|-------\n  platform-tools | 27.0.0 | Android SDK Platform-Tools | platform-tools\n",
		},
	})
	res, err := moduleAndroidSdk(context.Background(), conn, map[string]any{
		"package": []any{"platform-tools"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no install run", conn.Commands)
	}
}

func TestModuleAndroidSdkAcceptLicenses(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sdkmanager --list_installed --channel=0":               {RC: 0, Stdout: "Installed packages:\n"},
		"sdkmanager --install 'build-tools;34.0.0' --channel=0": {RC: 0},
	})
	res, err := moduleAndroidSdk(context.Background(), conn, map[string]any{
		"name":            "build-tools;34.0.0",
		"accept_licenses": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Stdins[1] != "y\n" {
		t.Fatalf("stdin = %q, want license accepted", conn.Stdins[1])
	}
}

func TestModuleAndroidSdkAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sdkmanager --list_installed --channel=0": {
			RC:     0,
			Stdout: "Installed packages:\n  Path | Version | Description | Location\n  -------|---------|-------|-------\n  platform-tools | 27.0.0 | Android SDK Platform-Tools | platform-tools\n",
		},
		"sdkmanager --uninstall platform-tools --channel=0": {RC: 0},
	})
	res, err := moduleAndroidSdk(context.Background(), conn, map[string]any{
		"name":  "platform-tools",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["removed"].([]string)[0] != "platform-tools" {
		t.Fatalf("removed = %v", res.Extra["removed"])
	}
}

func TestModuleAndroidSdkDuplicateName(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleAndroidSdk(context.Background(), conn, map[string]any{
		"name": []any{"foo", "foo"},
	})
	if err == nil {
		t.Fatal("want error for repeated package name")
	}
}

func TestModuleAndroidSdkBadChannel(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleAndroidSdk(context.Background(), conn, map[string]any{
		"name":    "foo",
		"channel": "nightly",
	})
	if err == nil {
		t.Fatal("want error for invalid channel")
	}
}
