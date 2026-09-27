package modules

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestModuleCopyContent(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	conn := local()

	res, err := moduleCopy(context.Background(), conn, map[string]any{"content": "hello", "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed on first write")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("content = %q", data)
	}

	res2, err := moduleCopy(context.Background(), conn, map[string]any{"content": "hello", "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged on identical re-copy")
	}
}

func TestModuleCopySrc(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dest := filepath.Join(dir, "dest.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCopy(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleCopyMode(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	conn := local()
	_, err := moduleCopy(context.Background(), conn, map[string]any{"content": "x", "dest": dest, "mode": "0600"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestModuleCopyMkdirParents(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "nested", "deeper", "out.txt")
	conn := local()
	_, err := moduleCopy(context.Background(), conn, map[string]any{"content": "x", "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
}

func TestModuleCopyMissingSrcAndContent(t *testing.T) {
	conn := local()
	if _, err := moduleCopy(context.Background(), conn, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error when neither content nor src is given")
	}
}

func TestModuleCopySrcReadError(t *testing.T) {
	conn := local()
	_, err := moduleCopy(context.Background(), conn, map[string]any{
		"src": filepath.Join(t.TempDir(), "absent"), "dest": "/x",
	})
	if err == nil {
		t.Fatal("want error for unreadable src")
	}
}

// TestCopyRejectsSrcWithContent pins the two argument errors real
// ansible-core 2.21.4 raises, WITH its exact wording (measured, not
// paraphrased). The mutually-exclusive case is the one that mattered:
// this module used to accept src and content together, quietly write
// the content, and report ok — so a playbook naming a source file
// copied something else.
func TestCopyRejectsSrcWithContent(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"both", map[string]any{"dest": "/x", "src": "/etc/hosts", "content": "a"},
			"src and content are mutually exclusive"},
		{"neither", map[string]any{"dest": "/x"},
			"src (or content) is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := copySource(tc.args)
			if err == nil || err.Error() != tc.want {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// A new file gets the mode the umask implies, not the 0600 of the
// staging file. Measured against real: copy with no mode: produces 0644
// under the usual umask 022, where this port produced 0600. An existing
// file keeps whatever mode it had -- also measured, on both sides -- so
// a write must not reset it.
func TestCopyNewFileTakesTheUmaskDefault(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	conn := local()

	fresh := filepath.Join(dir, "fresh.txt")
	if _, err := moduleCopy(ctx, conn, map[string]any{
		"content": "x\n", "dest": fresh,
	}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(fresh)
	if err != nil {
		t.Fatal(err)
	}
	// 0666 &^ umask. The test reads the umask the same way the module
	// does rather than hard-coding 0644, so it holds under any umask.
	res, err := conn.Exec(ctx, "umask", nil)
	if err != nil {
		t.Fatal(err)
	}
	mask, err := strconv.ParseUint(strings.TrimSpace(res.Stdout), 8, 32)
	if err != nil {
		t.Fatal(err)
	}
	want := os.FileMode(0o666 & ^uint32(mask))
	if fi.Mode().Perm() != want {
		t.Errorf("new file mode = %04o, want %04o", fi.Mode().Perm(), want)
	}

	// An explicit mode still wins.
	explicit := filepath.Join(dir, "explicit.txt")
	if _, err := moduleCopy(ctx, conn, map[string]any{
		"content": "x\n", "dest": explicit, "mode": "0755",
	}); err != nil {
		t.Fatal(err)
	}
	if fi, err = os.Stat(explicit); err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("explicit mode = %04o, want 0755", fi.Mode().Perm())
	}

	// And rewriting an existing file leaves its mode alone.
	if err := os.Chmod(fresh, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := moduleCopy(ctx, conn, map[string]any{
		"content": "different\n", "dest": fresh,
	}); err != nil {
		t.Fatal(err)
	}
	if fi, err = os.Stat(fresh); err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("rewritten file mode = %04o, want the 0640 it had", fi.Mode().Perm())
	}
}
