package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSwdepotPresentInstalls(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swlist -a revision -l product unzip":                                {RC: 1},
		"swinstall -x mount_all_filesystems=false -s repository:/path unzip": {RC: 0},
	})
	res, err := moduleSwdepot(context.Background(), conn, map[string]any{
		"name": "unzip", "state": "present", "depot": "repository:/path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSwdepotPresentAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swlist -a revision -l product unzip": {RC: 0, Stdout: "unzip           6.0    Info about unzip\n"},
	})
	res, err := moduleSwdepot(context.Background(), conn, map[string]any{
		"name": "unzip", "state": "present", "depot": "repository:/path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSwdepotLatestUpgrades(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swlist -a revision -l product unzip":                                {RC: 0, Stdout: "unzip           6.0    Info\n"},
		"swlist -a revision -l product -s repository:/path unzip":            {RC: 0, Stdout: "unzip           6.1    Info\n"},
		"swinstall -x mount_all_filesystems=false -s repository:/path unzip": {RC: 0},
	})
	res, err := moduleSwdepot(context.Background(), conn, map[string]any{
		"name": "unzip", "state": "latest", "depot": "repository:/path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed (6.0 -> 6.1), res = %+v", res)
	}
}

func TestModuleSwdepotAbsentRemoves(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swlist -a revision -l product unzip": {RC: 0, Stdout: "unzip           6.0    Info\n"},
		"swremove unzip":                      {RC: 0},
	})
	res, err := moduleSwdepot(context.Background(), conn, map[string]any{"name": "unzip", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSwdepotAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swlist -a revision -l product unzip": {RC: 1},
	})
	res, err := moduleSwdepot(context.Background(), conn, map[string]any{"name": "unzip", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if res.Msg != "No changed" {
		t.Fatalf("msg = %q, want real swdepot's own verbatim message", res.Msg)
	}
}

func TestModuleSwdepotMissingDepot(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSwdepot(context.Background(), conn, map[string]any{"name": "unzip", "state": "present"}); err == nil {
		t.Fatal("want error when depot is missing for state=present")
	}
}

func TestModuleSwdepotVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"6.0", "6.1", -1},
		{"6.1", "6.0", 1},
		{"6.0", "6.0.0", 0},
		{"1.2.3", "1.2.3", 0},
	}
	for _, c := range cases {
		if got := swdepotCompareVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
