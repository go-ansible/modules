package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHgClone(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.hg/hgrc":                          {RC: 1},
		"hg clone https://bitbucket.org/user/repo1 /srv/checkout": {RC: 0},
		"hg id -b -i -t -R /srv/checkout":                         {RC: 0, Stdout: "abcdef1234 stable\n"},
	})
	res, err := moduleHg(context.Background(), fc, map[string]any{
		"repo": "https://bitbucket.org/user/repo1", "dest": "/srv/checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHgCloneFalseSkips(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.hg/hgrc": {RC: 1},
	})
	res, err := moduleHg(context.Background(), fc, map[string]any{
		"repo": "https://bitbucket.org/user/repo", "dest": "/srv/checkout", "clone": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when clone=false and repo not yet cloned")
	}
}

func TestModuleHgNoCloneNoUpdate(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"hg id git://bitbucket.org/user/repo": {RC: 0, Stdout: "abcdef1234\n"},
	})
	res, err := moduleHg(context.Background(), fc, map[string]any{
		"repo": "git://bitbucket.org/user/repo", "dest": "/srv/checkout", "clone": false, "update": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: clone=false, update=false is a read-only query")
	}
	if res.Extra["after"] != "abcdef1234" {
		t.Fatalf("after = %q", res.Extra["after"])
	}
	if len(fc.Commands) != 1 {
		t.Fatalf("commands = %v, want exactly one remote query", fc.Commands)
	}
}

func TestModuleHgPullAndUpdate(t *testing.T) {
	revCmd := "hg id -b -i -t -R /srv/checkout"
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.hg/hgrc": {RC: 0},
		revCmd:                           {RC: 0, Stdout: "aaa111 stable\n"},
		"hg pull -R /srv/checkout https://bitbucket.org/user/repo1": {RC: 0},
		"hg update -r stable -R /srv/checkout":                      {RC: 0},
	})
	res, err := moduleHg(context.Background(), fc, map[string]any{
		"repo": "https://bitbucket.org/user/repo1", "dest": "/srv/checkout", "revision": "stable",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range fc.Commands {
		if c == "hg pull -R /srv/checkout https://bitbucket.org/user/repo1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want a pull", fc.Commands)
	}
}

func TestModuleHgAtRevisionSkipsPull(t *testing.T) {
	revCmd := "hg id -b -i -t -R /srv/checkout"
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.hg/hgrc":    {RC: 0},
		"hg --debug id -i -R /srv/checkout": {RC: 0, Stdout: "abcdef1234567890\n"},
		revCmd:                              {RC: 0, Stdout: "abcdef1234567890 stable\n"},
	})
	res, err := moduleHg(context.Background(), fc, map[string]any{
		"repo": "https://bitbucket.org/user/repo1", "dest": "/srv/checkout", "revision": "abcdef1234567890",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when already at revision")
	}
	for _, c := range fc.Commands {
		if c == "hg pull -R /srv/checkout https://bitbucket.org/user/repo1" {
			t.Fatal("want no pull when already at revision")
		}
	}
}

func TestModuleHgUpdateFalse(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/checkout/.hg/hgrc":  {RC: 0},
		"hg id -b -i -t -R /srv/checkout": {RC: 0, Stdout: "aaa111 stable\n"},
	})
	res, err := moduleHg(context.Background(), fc, map[string]any{
		"repo": "https://bitbucket.org/user/repo1", "dest": "/srv/checkout", "update": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged with update=false against an already-cloned repo")
	}
}

func TestModuleHgMissingDest(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleHg(context.Background(), fc, map[string]any{"repo": "x"}); err == nil {
		t.Fatal("want error: dest required unless clone=false and update=false")
	}
}

func TestModuleHgMissingRepo(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleHg(context.Background(), fc, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error for missing repo")
	}
}
