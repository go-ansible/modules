package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleFlatpakInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak info --system org.example.App >/dev/null 2>&1": {RC: 1},
		"flatpak install -y --system flathub org.example.App":   {RC: 0},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{"name": "org.example.App"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleFlatpakAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak info --system org.example.App >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{"name": "org.example.App"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleFlatpakAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak info --system org.example.App >/dev/null 2>&1": {RC: 0},
		"flatpak uninstall -y --system org.example.App":         {RC: 0},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{"name": "org.example.App", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleFlatpakAbsentNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak info --system org.example.App >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{"name": "org.example.App", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleFlatpakLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak update -y --system org.example.App": {RC: 0},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{"name": "org.example.App", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no query for latest", conn.Commands)
	}
}

func TestModuleFlatpakUserMethod(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak info --user org.example.App >/dev/null 2>&1": {RC: 1},
		"flatpak install -y --user flathub org.example.App":   {RC: 0},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{"name": "org.example.App", "method": "user"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleFlatpakInvalidMethod(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFlatpak(context.Background(), conn, map[string]any{"name": "org.example.App", "method": "bogus"}); err == nil {
		t.Fatal("want error for invalid method")
	}
}

func TestModuleFlatpakFromURLInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak info --system org.example.App >/dev/null 2>&1":          {RC: 1},
		"flatpak install -y --system https://example.com/app.flatpakref": {RC: 0},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{
		"name": "org.example.App", "from_url": "https://example.com/app.flatpakref",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleFlatpakFromURLAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak info --system org.example.App >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{
		"name": "org.example.App", "from_url": "https://example.com/app.flatpakref",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleFlatpakFromURLMultipleNames(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFlatpak(context.Background(), conn, map[string]any{
		"name": []any{"a", "b"}, "from_url": "https://example.com/app.flatpakref",
	}); err == nil {
		t.Fatal("want error: from_url requires exactly one name")
	}
}

func TestModuleFlatpakFromURLWithNonPresentState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFlatpak(context.Background(), conn, map[string]any{
		"name": "org.example.App", "from_url": "https://example.com/app.flatpakref", "state": "absent",
	}); err == nil {
		t.Fatal("want error: from_url only valid with state=present")
	}
}

func TestModuleFlatpakMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFlatpak(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleFlatpakNameListInstallsEachSeparately(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"flatpak info --system a >/dev/null 2>&1": {RC: 1},
		"flatpak info --system b >/dev/null 2>&1": {RC: 1},
		"flatpak install -y --system flathub a":   {RC: 0},
		"flatpak install -y --system flathub b":   {RC: 0},
	})
	res, err := moduleFlatpak(context.Background(), conn, map[string]any{"name": []any{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 4 {
		t.Fatalf("commands = %v, want 2 queries + 2 separate installs", conn.Commands)
	}
}
