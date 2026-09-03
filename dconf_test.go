package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDconfReadUnset(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dconf":                  {RC: 0},
		"dconf read /org/gnome/desktop/foo": {RC: 0, Stdout: ""},
	})
	res, err := moduleDconf(context.Background(), conn, map[string]any{
		"key": "/org/gnome/desktop/foo", "state": "read",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["value"] != nil {
		t.Fatalf("value = %v, want nil for unset key", res.Extra["value"])
	}
}

func TestModuleDconfReadSet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dconf":                  {RC: 0},
		"dconf read /org/gnome/desktop/foo": {RC: 0, Stdout: "'bar'\n"},
	})
	res, err := moduleDconf(context.Background(), conn, map[string]any{
		"key": "/org/gnome/desktop/foo", "state": "read",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != "'bar'" {
		t.Fatalf("value = %q", res.Extra["value"])
	}
}

func TestModuleDconfPresentUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dconf":                  {RC: 0},
		"dconf read /org/gnome/desktop/foo": {RC: 0, Stdout: "true\n"},
	})
	res, err := moduleDconf(context.Background(), conn, map[string]any{
		"key": "/org/gnome/desktop/foo", "state": "present", "value": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDconfPresentWritesViaExistingSession(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dconf":                        {RC: 0},
		"dconf read /org/gnome/desktop/foo":       {RC: 0, Stdout: ""},
		`printf '%s' "$DBUS_SESSION_BUS_ADDRESS"`: {RC: 0, Stdout: "unix:path=/run/user/1000/bus"},
		"DBUS_SESSION_BUS_ADDRESS='unix:path=/run/user/1000/bus' dconf write /org/gnome/desktop/foo 'true'": {RC: 0},
	})
	res, err := moduleDconf(context.Background(), conn, map[string]any{
		"key": "/org/gnome/desktop/foo", "state": "present", "value": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleDconfPresentWritesViaDbusRunSession(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dconf":                        {RC: 0},
		"dconf read /org/gnome/desktop/foo":       {RC: 0, Stdout: ""},
		`printf '%s' "$DBUS_SESSION_BUS_ADDRESS"`: {RC: 0, Stdout: ""},
		"id -u":                       {RC: 0, Stdout: "1000\n"},
		"test -e /run/user/1000/bus":  {RC: 1},
		"command -v dbus-run-session": {RC: 0},
		"dbus-run-session -- dconf write /org/gnome/desktop/foo 'true'": {RC: 0},
	})
	res, err := moduleDconf(context.Background(), conn, map[string]any{
		"key": "/org/gnome/desktop/foo", "state": "present", "value": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleDconfAbsentAlreadyUnset(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dconf":                  {RC: 0},
		"dconf read /org/gnome/desktop/foo": {RC: 0, Stdout: ""},
	})
	res, err := moduleDconf(context.Background(), conn, map[string]any{
		"key": "/org/gnome/desktop/foo", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleDconfMissingValue(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDconf(context.Background(), conn, map[string]any{
		"key": "/org/gnome/desktop/foo", "state": "present",
	}); err == nil {
		t.Fatal("want error for missing value with state=present")
	}
}

func TestModuleDconfNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dconf": {RC: 1},
	})
	res, err := moduleDconf(context.Background(), conn, map[string]any{
		"key": "/org/gnome/desktop/foo", "state": "read",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when dconf is not on the target")
	}
}
