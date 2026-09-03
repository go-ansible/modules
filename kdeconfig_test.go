package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKdeconfigWritesNewFile(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/xdg/kickoffrc":               {RC: 1},
		"command -v kwriteconfig6 >/dev/null 2>&1": {RC: 0},
		"kwriteconfig6 --file /tmp/kdeconfig --key Homepage --group Branding -- https://www.ansible.com/": {RC: 0},
		"cat /tmp/kdeconfig":                   {RC: 0, Stdout: "[Branding]\nHomepage=https://www.ansible.com/\n"},
		"mv /tmp/kdeconfig /etc/xdg/kickoffrc": {RC: 0},
	})
	res, err := moduleKdeconfig(context.Background(), conn, map[string]any{
		"path": "/etc/xdg/kickoffrc",
		"values": []any{
			map[string]any{"group": "Branding", "key": "Homepage", "value": "https://www.ansible.com/"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKdeconfigNestedGroupsAndBool(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/xdg/someconfigrc":            {RC: 1},
		"command -v kwriteconfig6 >/dev/null 2>&1": {RC: 0},
		"kwriteconfig6 --file /tmp/kdeconfig --key KEY --group Group --group Subgroup --type bool true": {RC: 0},
		"cat /tmp/kdeconfig":                      {RC: 0, Stdout: "[Group][Subgroup]\nKEY=true\n"},
		"mv /tmp/kdeconfig /etc/xdg/someconfigrc": {RC: 0},
	})
	res, err := moduleKdeconfig(context.Background(), conn, map[string]any{
		"path": "/etc/xdg/someconfigrc",
		"values": []any{
			map[string]any{"groups": []any{"Group", "Subgroup"}, "key": "KEY", "bool_value": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKdeconfigUnchangedWhenContentSame(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/xdg/kickoffrc":               {RC: 0},
		"cat /etc/xdg/kickoffrc":                   {RC: 0, Stdout: "[Branding]\nHomepage=https://www.ansible.com/\n"},
		"command -v kwriteconfig6 >/dev/null 2>&1": {RC: 0},
		"kwriteconfig6 --file /tmp/kdeconfig --key Homepage --group Branding -- https://www.ansible.com/": {RC: 0},
		"cat /tmp/kdeconfig": {RC: 0, Stdout: "[Branding]\nHomepage=https://www.ansible.com/\n"},
	})
	res, err := moduleKdeconfig(context.Background(), conn, map[string]any{
		"path": "/etc/xdg/kickoffrc",
		"values": []any{
			map[string]any{"group": "Branding", "key": "Homepage", "value": "https://www.ansible.com/"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKdeconfigEmptyKeyFails(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleKdeconfig(context.Background(), conn, map[string]any{
		"path": "/etc/xdg/kickoffrc",
		"values": []any{
			map[string]any{"group": "Branding", "key": "", "value": "x"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for empty key")
	}
}

func TestModuleKdeconfigNoKwriteconfig(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kwriteconfig6 >/dev/null 2>&1": {RC: 1},
		"command -v kwriteconfig5 >/dev/null 2>&1": {RC: 1},
		"command -v kwriteconfig >/dev/null 2>&1":  {RC: 1},
		"command -v kwriteconfig4 >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleKdeconfig(context.Background(), conn, map[string]any{
		"path": "/etc/xdg/kickoffrc",
		"values": []any{
			map[string]any{"group": "Branding", "key": "Homepage", "value": "x"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when no kwriteconfig variant is installed")
	}
}

func TestModuleKdeconfigMissingValues(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKdeconfig(context.Background(), conn, map[string]any{"path": "/tmp/x"}); err == nil {
		t.Fatal("want error for missing values")
	}
}

func TestModuleKdeconfigMissingPath(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKdeconfig(context.Background(), conn, map[string]any{
		"values": []any{map[string]any{"group": "G", "key": "K", "value": "V"}},
	}); err == nil {
		t.Fatal("want error for missing path")
	}
}
