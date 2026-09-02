package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestYumRepoStanza(t *testing.T) {
	s := yumRepoStanza("epel", "Extra Packages", []string{"https://example.com/epel"}, nil, nil, nil)
	want := "[epel]\nname=Extra Packages\nbaseurl=https://example.com/epel\n"
	if s != want {
		t.Fatalf("stanza = %q, want %q", s, want)
	}

	gpgcheck := true
	enabled := false
	full := yumRepoStanza("epel", "Extra Packages", []string{"https://example.com/epel"}, &gpgcheck, []string{"https://example.com/key"}, &enabled)
	wantFull := "[epel]\nname=Extra Packages\nbaseurl=https://example.com/epel\ngpgcheck=1\ngpgkey=https://example.com/key\nenabled=0\n"
	if full != wantFull {
		t.Fatalf("stanza = %q, want %q", full, wantFull)
	}
}

func TestModuleYumRepositoryNew(t *testing.T) {
	path := "/etc/yum.repos.d/epel.repo"
	stanza := yumRepoStanza("epel", "Extra Packages", []string{"https://example.com/epel"}, nil, nil, nil)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 1},
		"mkdir -p /etc/yum.repos.d && printf '%s' " + shellQuote(stanza) + " > " + shellQuote(path): {RC: 0},
	})
	res, err := moduleYumRepository(context.Background(), conn, map[string]any{
		"name": "epel", "description": "Extra Packages", "baseurl": []string{"https://example.com/epel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYumRepositoryAlreadyPresent(t *testing.T) {
	path := "/etc/yum.repos.d/epel.repo"
	stanza := yumRepoStanza("epel", "Extra Packages", []string{"https://example.com/epel"}, nil, nil, nil)
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 0},
		"cat " + shellQuote(path):     {RC: 0, Stdout: "[epel]\nname=Extra Packages\nbaseurl=https://example.com/epel"},
	})
	res, err := moduleYumRepository(context.Background(), conn, map[string]any{
		"name": "epel", "description": "Extra Packages", "baseurl": []string{"https://example.com/epel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	_ = stanza
}

func TestModuleYumRepositoryCustomFile(t *testing.T) {
	path := "/etc/yum.repos.d/custom.repo"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 1},
		"mkdir -p /etc/yum.repos.d && printf '%s' " + shellQuote(yumRepoStanza("epel", "Extra Packages", []string{"https://example.com/epel"}, nil, nil, nil)) + " > " + shellQuote(path): {RC: 0},
	})
	res, err := moduleYumRepository(context.Background(), conn, map[string]any{
		"name": "epel", "file": "custom", "description": "Extra Packages", "baseurl": []string{"https://example.com/epel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYumRepositoryAbsent(t *testing.T) {
	path := "/etc/yum.repos.d/epel.repo"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 0},
		"rm -f " + shellQuote(path):   {RC: 0},
	})
	res, err := moduleYumRepository(context.Background(), conn, map[string]any{"name": "epel", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleYumRepositoryAbsentAlreadyGone(t *testing.T) {
	path := "/etc/yum.repos.d/epel.repo"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(path): {RC: 1},
	})
	res, err := moduleYumRepository(context.Background(), conn, map[string]any{"name": "epel", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleYumRepositoryValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleYumRepository(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleYumRepository(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
	if _, err := moduleYumRepository(context.Background(), conn, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error for missing description")
	}
	if _, err := moduleYumRepository(context.Background(), conn, map[string]any{"name": "x", "description": "d"}); err == nil {
		t.Fatal("want error for missing baseurl")
	}
}
