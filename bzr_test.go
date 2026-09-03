package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleBzrClone(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.bzr/branch/branch.conf": {RC: 1},
		"mkdir -p /srv": {RC: 0},
		"bzr branch bzr+ssh://foosball.example.org/path/to/branch /srv/checkout": {RC: 0},
		"cd /srv/checkout && bzr revert":                                         {RC: 0},
		"cd /srv/checkout && bzr revno":                                          {RC: 0, Stdout: "5\n"},
	})
	res, err := moduleBzr(context.Background(), fc, map[string]any{
		"name": "bzr+ssh://foosball.example.org/path/to/branch",
		"dest": "/srv/checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["before"] != "" || res.Extra["after"] != "5" {
		t.Fatalf("before/after = %q/%q", res.Extra["before"], res.Extra["after"])
	}
}

func TestModuleBzrCloneWithVersion(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.bzr/branch/branch.conf": {RC: 1},
		"mkdir -p /srv": {RC: 0},
		"bzr branch -r 22 bzr+ssh://foosball.example.org/path/to/branch /srv/checkout": {RC: 0},
		"cd /srv/checkout && bzr revert -r 22":                                         {RC: 0},
		"cd /srv/checkout && bzr revno":                                                {RC: 0, Stdout: "22\n"},
	})
	res, err := moduleBzr(context.Background(), fc, map[string]any{
		"name":    "bzr+ssh://foosball.example.org/path/to/branch",
		"dest":    "/srv/checkout",
		"version": "22",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleBzrPullNoChange(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.bzr/branch/branch.conf": {RC: 0},
		"cd /srv/checkout && bzr status -S":             {RC: 0, Stdout: ""},
		"cd /srv/checkout && bzr revno":                 {RC: 0, Stdout: "5\n"},
		"cd /srv/checkout && bzr revert":                {RC: 0},
		"cd /srv/checkout && bzr pull":                  {RC: 0},
	})
	res, err := moduleBzr(context.Background(), fc, map[string]any{
		"name": "bzr+ssh://foosball.example.org/path/to/branch",
		"dest": "/srv/checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleBzrLocalModsForceFalse(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.bzr/branch/branch.conf": {RC: 0},
		"cd /srv/checkout && bzr status -S":             {RC: 0, Stdout: "N  file.txt\n"},
		"cd /srv/checkout && bzr revno":                 {RC: 0, Stdout: "5\n"},
	})
	res, err := moduleBzr(context.Background(), fc, map[string]any{
		"name": "bzr+ssh://foosball.example.org/path/to/branch",
		"dest": "/srv/checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when local modifications exist and force=false")
	}
}

func TestModuleBzrLocalModsForceTrue(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.bzr/branch/branch.conf": {RC: 0},
		"cd /srv/checkout && bzr status -S":             {RC: 0, Stdout: "N  file.txt\n"},
		"cd /srv/checkout && bzr revno":                 {RC: 0, Stdout: "5\n"},
		"cd /srv/checkout && bzr revert":                {RC: 0},
		"cd /srv/checkout && bzr pull":                  {RC: 0},
	})
	res, err := moduleBzr(context.Background(), fc, map[string]any{
		"name": "bzr+ssh://foosball.example.org/path/to/branch", "dest": "/srv/checkout", "force": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// localMods was true, so changed regardless of before==after.
	if !res.Changed {
		t.Fatal("want changed when local modifications were discarded")
	}
}

func TestModuleBzrMissingArgs(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleBzr(context.Background(), fc, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleBzr(context.Background(), fc, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}
