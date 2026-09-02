package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestUnarchiveCmd(t *testing.T) {
	cases := map[string]string{
		"/a/x.tar":     "tar xf /a/x.tar -C /dest",
		"/a/x.tar.gz":  "tar xzf /a/x.tar.gz -C /dest",
		"/a/x.tgz":     "tar xzf /a/x.tgz -C /dest",
		"/a/x.tar.bz2": "tar xjf /a/x.tar.bz2 -C /dest",
		"/a/x.tbz2":    "tar xjf /a/x.tbz2 -C /dest",
		"/a/x.tar.xz":  "tar xJf /a/x.tar.xz -C /dest",
		"/a/x.txz":     "tar xJf /a/x.txz -C /dest",
		"/a/x.zip":     "unzip -o /a/x.zip -d /dest",
	}
	for in, want := range cases {
		got, err := unarchiveCmd(in, "/dest")
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Errorf("unarchiveCmd(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := unarchiveCmd("/a/x.rar", "/dest"); err == nil {
		t.Fatal("want error for unrecognized extension")
	}
}

func mustLocalArchive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "bundle.tar.gz")
	if err := os.WriteFile(p, []byte("fake archive bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestModuleUnarchiveLocalSrc(t *testing.T) {
	local := mustLocalArchive(t)
	remote := "/tmp/bundle.tar.gz"
	conn := newFakeConn(map[string]remoteexec.Result{
		"tar xzf " + shellQuote(remote) + " -C " + shellQuote("/dest"): {RC: 0},
	})
	res, err := moduleUnarchive(context.Background(), conn, map[string]any{"src": local, "dest": "/dest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleUnarchiveRemoteSrc(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"unzip -o " + shellQuote("/opt/bundle.zip") + " -d " + shellQuote("/dest"): {RC: 0},
	})
	res, err := moduleUnarchive(context.Background(), conn, map[string]any{
		"src": "/opt/bundle.zip", "dest": "/dest", "remote_src": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	// remote_src=true must not attempt a Put.
	if len(conn.Commands) != 1 {
		t.Fatalf("Commands = %v", conn.Commands)
	}
}

func TestModuleUnarchiveCreatesSkip(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote("/dest/marker"): {RC: 0},
	})
	res, err := moduleUnarchive(context.Background(), conn, map[string]any{
		"src": "/opt/bundle.zip", "dest": "/dest", "remote_src": true, "creates": "/dest/marker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged (skipped)")
	}
}

func TestModuleUnarchiveMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUnarchive(context.Background(), conn, map[string]any{"dest": "/d"}); err == nil {
		t.Fatal("want error for missing src")
	}
	if _, err := moduleUnarchive(context.Background(), conn, map[string]any{"src": "/s.zip"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}
