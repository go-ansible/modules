package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHtpasswdBinaryMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd": {RC: 1},
	})
	res, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": "/etc/passwdfile", "name": "jane", "password": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: htpasswd binary missing")
	}
}

func TestModuleHtpasswdCreateNewFile(t *testing.T) {
	path, name, password := "/etc/passwdfile", "jane", "secret"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd":         {RC: 0},
		"test -e " + shellQuote(path): {RC: 1},
		"htpasswd -b -c -m " + shellQuote(path) + " " + shellQuote(name) + " " + shellQuote(password): {RC: 0},
	})
	res, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": name, "password": password,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHtpasswdAlreadyMatches(t *testing.T) {
	path, name, password := "/etc/passwdfile", "jane", "secret"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd":         {RC: 0},
		"test -e " + shellQuote(path): {RC: 0},
		"htpasswd -vb " + shellQuote(path) + " " + shellQuote(name) + " " + shellQuote(password) + " 2>/dev/null": {RC: 0},
	})
	res, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": name, "password": password,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHtpasswdUpdatePassword(t *testing.T) {
	path, name, password := "/etc/passwdfile", "jane", "newsecret"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd":         {RC: 0},
		"test -e " + shellQuote(path): {RC: 0},
		"htpasswd -vb " + shellQuote(path) + " " + shellQuote(name) + " " + shellQuote(password) + " 2>/dev/null": {RC: 1},
		"htpasswd -b -m " + shellQuote(path) + " " + shellQuote(name) + " " + shellQuote(password):                {RC: 0},
	})
	res, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": name, "password": password,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHtpasswdAbsentPresent(t *testing.T) {
	path, name := "/etc/passwdfile", "jane"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd": {RC: 0},
		"awk -F: -v u=" + shellQuote(name) + " '$1==u{f=1} END{exit !f}' " + shellQuote(path) + " 2>/dev/null": {RC: 0},
		"htpasswd -D " + shellQuote(path) + " " + shellQuote(name):                                             {RC: 0},
	})
	res, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHtpasswdAbsentNotPresent(t *testing.T) {
	path, name := "/etc/passwdfile", "jane"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd": {RC: 0},
		"awk -F: -v u=" + shellQuote(name) + " '$1==u{f=1} END{exit !f}' " + shellQuote(path) + " 2>/dev/null": {RC: 1},
	})
	res, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHtpasswdCreateFalseMissingFile(t *testing.T) {
	path := "/etc/passwdfile"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd":         {RC: 0},
		"test -e " + shellQuote(path): {RC: 1},
	})
	res, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": "jane", "password": "secret", "create": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: create is false and file does not exist")
	}
}

func TestModuleHtpasswdPasswordOmittedExisting(t *testing.T) {
	path, name := "/etc/passwdfile", "jane"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd":         {RC: 0},
		"test -e " + shellQuote(path): {RC: 0},
		"awk -F: -v u=" + shellQuote(name) + " '$1==u{f=1} END{exit !f}' " + shellQuote(path) + " 2>/dev/null": {RC: 0},
	})
	res, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHtpasswdPasswordOmittedMissingUser(t *testing.T) {
	path, name := "/etc/passwdfile", "jane"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd":         {RC: 0},
		"test -e " + shellQuote(path): {RC: 0},
		"awk -F: -v u=" + shellQuote(name) + " '$1==u{f=1} END{exit !f}' " + shellQuote(path) + " 2>/dev/null": {RC: 1},
	})
	if _, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": name,
	}); err == nil {
		t.Fatal("want error: password required to create a new entry")
	}
}

func TestModuleHtpasswdInvalidHashScheme(t *testing.T) {
	path := "/etc/passwdfile"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v htpasswd":         {RC: 0},
		"test -e " + shellQuote(path): {RC: 0},
	})
	if _, err := moduleHtpasswd(context.Background(), conn, map[string]any{
		"path": path, "name": "jane", "password": "secret", "hash_scheme": "sha256_crypt",
	}); err == nil {
		t.Fatal("want error for unsupported hash_scheme")
	}
}

func TestModuleHtpasswdValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHtpasswd(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
	if _, err := moduleHtpasswd(context.Background(), conn, map[string]any{"path": "/x"}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleHtpasswd(context.Background(), conn, map[string]any{"path": "/x", "name": "y", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
