package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleFlatpakRemoteAddNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak remotes --system --columns=name 2>/dev/null | grep -qxF flathub":          {RC: 1},
		"flatpak remote-add --system flathub https://flathub.org/repo/flathub.flatpakrepo": {RC: 0},
	})
	res, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{
		"name": "flathub", "flatpakrepo_url": "https://flathub.org/repo/flathub.flatpakrepo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleFlatpakRemoteAlreadyAdded(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak remotes --system --columns=name 2>/dev/null | grep -qxF flathub": {RC: 0},
	})
	res, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{
		"name": "flathub", "flatpakrepo_url": "https://flathub.org/repo/flathub.flatpakrepo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: real flatpak_remote does not update an existing remote")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no remote-add attempted, commands = %v", conn.Commands)
	}
}

func TestModuleFlatpakRemoteMissingURLWhenNotPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak remotes --system --columns=name 2>/dev/null | grep -qxF flathub": {RC: 1},
	})
	if _, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{"name": "flathub"}); err == nil {
		t.Fatal("want error: flatpakrepo_url required when adding")
	}
}

func TestModuleFlatpakRemoteAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak remotes --system --columns=name 2>/dev/null | grep -qxF flathub": {RC: 0},
		"flatpak remote-delete --system flathub":                                  {RC: 0},
	})
	res, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{"name": "flathub", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleFlatpakRemoteAbsentNotPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak remotes --system --columns=name 2>/dev/null | grep -qxF flathub": {RC: 1},
	})
	res, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{"name": "flathub", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleFlatpakRemoteInvalidMethod(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{"name": "flathub", "method": "bogus"}); err == nil {
		t.Fatal("want error for invalid method")
	}
}

func TestModuleFlatpakRemoteMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleFlatpakRemoteInvalidState(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak remotes --system --columns=name 2>/dev/null | grep -qxF flathub": {RC: 1},
	})
	if _, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{"name": "flathub", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleFlatpakRemoteUserMethod(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak remotes --user --columns=name 2>/dev/null | grep -qxF flathub":          {RC: 1},
		"flatpak remote-add --user flathub https://flathub.org/repo/flathub.flatpakrepo": {RC: 0},
	})
	res, err := moduleFlatpakRemote(context.Background(), conn, map[string]any{
		"name": "flathub", "flatpakrepo_url": "https://flathub.org/repo/flathub.flatpakrepo", "method": "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
