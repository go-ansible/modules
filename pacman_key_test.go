package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePacmanKeyMissingID(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePacmanKey(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing id")
	}
}

func TestModulePacmanKeyAbsent(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 0},
		"pacman-key --gpgdir /etc/pacman.d/gnupg --delete " + id:                         {RC: 0},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePacmanKeyAbsentNotPresent(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 1},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePacmanKeyPresentViaData(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	keyData := "-----BEGIN PGP PUBLIC KEY BLOCK-----\n...\n-----END PGP PUBLIC KEY BLOCK-----\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 1},
		"pacman-key --gpgdir /etc/pacman.d/gnupg --add -":                                {RC: 0},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "data": keyData})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	found := false
	for i, c := range conn.Commands {
		if c == "pacman-key --gpgdir /etc/pacman.d/gnupg --add -" {
			found = true
			if conn.Stdins[i] != keyData {
				t.Fatalf("stdin = %q, want %q", conn.Stdins[i], keyData)
			}
		}
	}
	if !found {
		t.Fatalf("--add - not invoked, commands = %v", conn.Commands)
	}
}

func TestModulePacmanKeyPresentViaDataFailure(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 1},
		"pacman-key --gpgdir /etc/pacman.d/gnupg --add -":                                {RC: 2, Stderr: "invalid key material"},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "data": "garbage"})
	if err != nil {
		t.Fatalf("want a Result{Failed:true}, not a Go error: %v", err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModulePacmanKeyPresentViaFile(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 1},
		"pacman-key --gpgdir /etc/pacman.d/gnupg --add /tmp/key.asc":                     {RC: 0},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "file": "/tmp/key.asc"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacmanKeyPresentViaKeyserver(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1":             {RC: 1},
		"pacman-key --gpgdir /etc/pacman.d/gnupg --keyserver keyserver.ubuntu.com --recv-keys " + id: {RC: 0},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "keyserver": "keyserver.ubuntu.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePacmanKeyPresentNoSourceErrors(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 1},
	})
	if _, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id}); err == nil {
		t.Fatal("want error: one of data, file, or keyserver required")
	}
}

func TestModulePacmanKeyAlreadyPresentNoop(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want only the presence check, commands = %v", conn.Commands)
	}
}

func TestModulePacmanKeyEnsureTrustedOnAlreadyPresentKey(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 0},
		"pacman-key --gpgdir /etc/pacman.d/gnupg --lsign-key " + id:                      {RC: 0},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "ensure_trusted": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed: ensure_trusted still lsigns even when the key is already present, res = %+v", res)
	}
}

func TestModulePacmanKeyEnsureTrustedOnNewKey(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 1},
		"pacman-key --gpgdir /etc/pacman.d/gnupg --add /tmp/key.asc":                     {RC: 0},
		"pacman-key --gpgdir /etc/pacman.d/gnupg --lsign-key " + id:                      {RC: 0},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{
		"id": id, "file": "/tmp/key.asc", "ensure_trusted": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 3 {
		t.Fatalf("commands = %v, want check+add+lsign", conn.Commands)
	}
}

func TestModulePacmanKeyCustomKeyring(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /custom/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "keyring": "/custom/gnupg"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePacmanKeyInvalidState(t *testing.T) {
	id := "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234"
	conn := newFakeConn(map[string]remoteexec.Result{
		"pacman-key --gpgdir /etc/pacman.d/gnupg --list-keys " + id + " >/dev/null 2>&1": {RC: 1},
	})
	if _, err := modulePacmanKey(context.Background(), conn, map[string]any{"id": id, "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
