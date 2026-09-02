package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestDeb822Stanza(t *testing.T) {
	s := deb822Stanza([]string{"deb"}, []string{"https://example.com"}, []string{"stable"}, []string{"main"}, "")
	want := "Types: deb\nURIs: https://example.com\nSuites: stable\nComponents: main\n"
	if s != want {
		t.Fatalf("stanza = %q, want %q", s, want)
	}

	withKey := deb822Stanza([]string{"deb"}, []string{"https://example.com"}, []string{"stable"}, nil, "/etc/apt/keyrings/x.gpg")
	wantKey := "Types: deb\nURIs: https://example.com\nSuites: stable\nSigned-By: /etc/apt/keyrings/x.gpg\n"
	if withKey != wantKey {
		t.Fatalf("stanza = %q, want %q", withKey, wantKey)
	}
}

func TestModuleDeb822RepositoryNew(t *testing.T) {
	path := "/etc/apt/sources.list.d/example.sources"
	stanza := deb822Stanza([]string{"deb"}, []string{"https://example.com"}, []string{"stable"}, []string{"main"}, "")
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 1},
		"mkdir -p /etc/apt/sources.list.d && printf '%s' " + shellQuote(stanza) + " > " + shellQuote(path): {RC: 0},
	})
	res, err := moduleDeb822Repository(context.Background(), conn, map[string]any{
		"name": "example", "uris": []string{"https://example.com"}, "suites": []string{"stable"}, "components": []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDeb822RepositoryAlreadyPresent(t *testing.T) {
	path := "/etc/apt/sources.list.d/example.sources"
	stanza := deb822Stanza([]string{"deb"}, []string{"https://example.com"}, []string{"stable"}, []string{"main"}, "")
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 0},
		"cat " + shellQuote(path):     {RC: 0, Stdout: "Types: deb\nURIs: https://example.com\nSuites: stable\nComponents: main"},
	})
	res, err := moduleDeb822Repository(context.Background(), conn, map[string]any{
		"name": "example", "uris": []string{"https://example.com"}, "suites": []string{"stable"}, "components": []string{"main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	_ = stanza
}

func TestModuleDeb822RepositoryAbsent(t *testing.T) {
	path := "/etc/apt/sources.list.d/example.sources"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 0},
		"rm -f " + shellQuote(path):   {RC: 0},
	})
	res, err := moduleDeb822Repository(context.Background(), conn, map[string]any{"name": "example", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDeb822RepositoryAbsentAlreadyGone(t *testing.T) {
	path := "/etc/apt/sources.list.d/example.sources"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 1},
	})
	res, err := moduleDeb822Repository(context.Background(), conn, map[string]any{"name": "example", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleDeb822RepositoryValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDeb822Repository(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleDeb822Repository(context.Background(), conn, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error for missing uris")
	}
	if _, err := moduleDeb822Repository(context.Background(), conn, map[string]any{"name": "x", "uris": []string{"u"}}); err == nil {
		t.Fatal("want error for missing suites")
	}
	if _, err := moduleDeb822Repository(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
