package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSubversionCheckoutNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote("/d/.svn"):                                            {RC: 1},
		"svn checkout --quiet " + shellQuote("svn://x/repo") + " " + shellQuote("/d"): {RC: 0},
	})
	res, err := moduleSubversion(context.Background(), conn, map[string]any{"repo": "svn://x/repo", "dest": "/d"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSubversionCheckoutWithRevision(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote("/d/.svn"): {RC: 1},
		"svn checkout --quiet -r " + shellQuote("42") + " " + shellQuote("svn://x/repo") + " " + shellQuote("/d"): {RC: 0},
	})
	res, err := moduleSubversion(context.Background(), conn, map[string]any{
		"repo": "svn://x/repo", "dest": "/d", "revision": "42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSubversionUpdateUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote("/d/.svn"):       {RC: 0},
		"svnversion " + shellQuote("/d"):         {RC: 0, Stdout: "42"},
		"svn update --quiet " + shellQuote("/d"): {RC: 0},
	})
	res, err := moduleSubversion(context.Background(), conn, map[string]any{"repo": "svn://x/repo", "dest": "/d"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSubversionExport(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"svn export --quiet --force " + shellQuote("svn://x/repo") + " " + shellQuote("/d"): {RC: 0},
	})
	res, err := moduleSubversion(context.Background(), conn, map[string]any{
		"repo": "svn://x/repo", "dest": "/d", "export": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSubversionMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSubversion(context.Background(), conn, map[string]any{"dest": "/d"}); err == nil {
		t.Fatal("want error for missing repo")
	}
	if _, err := moduleSubversion(context.Background(), conn, map[string]any{"repo": "r"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}
