package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestShellDirname(t *testing.T) {
	cases := map[string]string{
		"/home/user/.ssh/known_hosts": "/home/user/.ssh",
		"known_hosts":                 ".",
		"/known_hosts":                ".",
	}
	for in, want := range cases {
		if got := shellDirname(in); got != want {
			t.Errorf("shellDirname(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModuleKnownHostsPresentNew(t *testing.T) {
	path := "~/.ssh/known_hosts"
	key := "example.com ssh-rsa AAAA..."
	conn := newFakeConn(map[string]remoteexec.Result{
		"grep -qxF " + shellQuote(key) + " " + shellQuote(path) + " 2>/dev/null":                                           {RC: 1},
		"mkdir -p " + shellQuote(shellDirname(path)) + " && printf '%s\\n' " + shellQuote(key) + " >> " + shellQuote(path): {RC: 0},
	})
	res, err := moduleKnownHosts(context.Background(), conn, map[string]any{"name": "example.com", "key": key})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKnownHostsPresentAlready(t *testing.T) {
	path := "~/.ssh/known_hosts"
	key := "example.com ssh-rsa AAAA..."
	conn := newFakeConn(map[string]remoteexec.Result{
		"grep -qxF " + shellQuote(key) + " " + shellQuote(path) + " 2>/dev/null": {RC: 0},
	})
	res, err := moduleKnownHosts(context.Background(), conn, map[string]any{"name": "example.com", "key": key})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKnownHostsAbsent(t *testing.T) {
	path := "~/.ssh/known_hosts"
	conn := newFakeConn(map[string]remoteexec.Result{
		"grep -q " + shellQuote("example.com") + " " + shellQuote(path) + " 2>/dev/null": {RC: 0},
		"grep -v " + shellQuote("example.com") + " " + shellQuote(path) + " > " + shellQuote(path+".tmp") +
			" && mv " + shellQuote(path+".tmp") + " " + shellQuote(path): {RC: 0},
	})
	res, err := moduleKnownHosts(context.Background(), conn, map[string]any{"name": "example.com", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKnownHostsAbsentNotPresent(t *testing.T) {
	path := "~/.ssh/known_hosts"
	conn := newFakeConn(map[string]remoteexec.Result{
		"grep -q " + shellQuote("example.com") + " " + shellQuote(path) + " 2>/dev/null": {RC: 1},
	})
	res, err := moduleKnownHosts(context.Background(), conn, map[string]any{"name": "example.com", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKnownHostsValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKnownHosts(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleKnownHosts(context.Background(), conn, map[string]any{"name": "h"}); err == nil {
		t.Fatal("want error for missing key when present")
	}
	if _, err := moduleKnownHosts(context.Background(), conn, map[string]any{"name": "h", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
