package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// Every expected value was measured against ansible-core 2.21.4, after
// reading add_path_info in module_utils/basic.py -- which is how the
// rule was found at all. wait_for's own exit_json names seven keys and
// real returns thirteen; no amount of reading wait_for.py explains the
// other six.

func pathInfoConn(t *testing.T) remoteexec.Connection {
	t.Helper()
	return remoteexec.NewLocal()
}

func TestAddPathInfoAddsSevenKeysForAnExistingPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := Ok("").WithExtra("path", p)
	out, err := addPathInfo(context.Background(), pathInfoConn(t), "wait_for", in)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"uid", "gid", "owner", "group", "mode", "state", "size"} {
		if _, ok := out.Extra[k]; !ok {
			t.Errorf("missing %q; real adds all seven", k)
		}
	}
	if out.Extra["state"] != "file" {
		t.Errorf("state = %#v, want file", out.Extra["state"])
	}
	if out.Extra["size"] != int64(5) {
		t.Errorf("size = %#v, want 5", out.Extra["size"])
	}
	if out.Extra["mode"] != "0644" {
		t.Errorf("mode = %#v, want 0644 (Python's '0%%03o')", out.Extra["mode"])
	}
}

// A path that is NOT there adds nothing at all -- real's add_path_info
// returns early when os.path.exists is false, and never fails for it.
func TestAddPathInfoIgnoresAMissingPath(t *testing.T) {
	in := Ok("").WithExtra("path", filepath.Join(t.TempDir(), "nosuch"))
	out, err := addPathInfo(context.Background(), pathInfoConn(t), "wait_for", in)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"uid", "gid", "owner", "group", "mode", "state", "size"} {
		if _, ok := out.Extra[k]; ok {
			t.Errorf("a missing path added %q; real adds nothing", k)
		}
	}
}

// state is OVERWRITTEN, not filled in. wait_for asks for
// `state: started` and real reports state=file.
func TestAddPathInfoOverwritesState(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := Ok("").WithExtra("path", p).WithExtra("state", "started")
	out, err := addPathInfo(context.Background(), pathInfoConn(t), "wait_for", in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Extra["state"] != "file" {
		t.Errorf("state = %#v, want file -- add_path_info REPLACES the module's own", out.Extra["state"])
	}
}

// The stat is an LSTAT: a symlink reports its own size and mode, and a
// file with more than one link reports state=hard.
func TestAddPathInfoUsesLstat(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tgt.txt")
	if err := os.WriteFile(target, []byte("target contents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "lnk.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(dir, "hard.txt")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}

	conn := pathInfoConn(t)
	for _, tc := range []struct {
		name, path, wantState string
		wantSize              int64
	}{
		{"a symlink is its own file", link, "link", int64(len(target))},
		{"a file with two links", hard, "hard", 16},
		{"a directory", dir, "directory", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := addPathInfo(context.Background(), conn, "wait_for", Ok("").WithExtra("path", tc.path))
			if err != nil {
				t.Fatal(err)
			}
			if out.Extra["state"] != tc.wantState {
				t.Errorf("state = %#v, want %q", out.Extra["state"], tc.wantState)
			}
			if tc.wantSize >= 0 && out.Extra["size"] != tc.wantSize {
				t.Errorf("size = %#v, want %d -- lstat reports the LINK, not its target", out.Extra["size"], tc.wantSize)
			}
		})
	}
}

// dest is looked at when path is absent, and path WINS when both are
// there -- real's own `kwargs.get('path', kwargs.get('dest'))`.
func TestAddPathInfoPrefersPathOverDest(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.txt")
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(small, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(big, []byte("xxxxxxxxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := pathInfoConn(t)

	out, err := addPathInfo(context.Background(), conn, "wait_for", Ok("").WithExtra("dest", big))
	if err != nil {
		t.Fatal(err)
	}
	if out.Extra["size"] != int64(10) {
		t.Errorf("dest alone: size = %#v, want 10", out.Extra["size"])
	}

	out, err = addPathInfo(context.Background(), conn, "wait_for", Ok("").WithExtra("path", small).WithExtra("dest", big))
	if err != nil {
		t.Fatal(err)
	}
	if out.Extra["size"] != int64(1) {
		t.Errorf("both given: size = %#v, want 1 -- path wins over dest", out.Extra["size"])
	}
}

// TestAddPathInfoSkipsControllerSideResults pins the exception. Real's
// fetch is an action plugin that builds its result in Python and ends
// with `return result` -- it never calls exit_json, so
// _return_formatted never runs on it. The corpus caught this port
// adding seven keys real does not have.
func TestAddPathInfoSkipsControllerSideResults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := Ok("").WithExtra("dest", p)

	out, err := addPathInfo(context.Background(), pathInfoConn(t), "fetch", in)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"uid", "gid", "owner", "group", "mode", "state", "size"} {
		if _, ok := out.Extra[k]; ok {
			t.Errorf("fetch gained %q; real's fetch result never goes through add_path_info", k)
		}
	}

	// And the control: any other module with the same result DOES get
	// them, so the skip is about the NAME and not about dest.
	out, err = addPathInfo(context.Background(), pathInfoConn(t), "copy", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Extra["size"]; !ok {
		t.Error("copy with the same result did NOT get path info; the exception is too wide")
	}
}
