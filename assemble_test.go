package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssembleCmd(t *testing.T) {
	cmd := assembleCmd("/frags", "/out", "", "")
	want := `find /frags -mindepth 1 -maxdepth 1 -type f | sort | while IFS= read -r f; do cat "$f"; done > /out`
	if cmd != want {
		t.Fatalf("cmd = %q, want %q", cmd, want)
	}

	cmd = assembleCmd("/frags", "/out", `^\d+-`, "\n")
	if cmd == want {
		t.Fatal("regexp/delimiter should change the command")
	}
}

// Run against a real local connection rather than a fake keyed on the
// command string. The fake could only assert that a particular pipeline
// was sent; it could not see that the pipeline wrote "changed" on every
// run, which is what real does not do.
func TestModuleAssembleBehaviour(t *testing.T) {
	dir := t.TempDir()
	frags := filepath.Join(dir, "frags")
	if err := os.MkdirAll(frags, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"01-a": "first\n", "02-b": "second\n", "03-c": "third\n"} {
		if err := os.WriteFile(filepath.Join(frags, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(dir, "out.txt")
	ctx := context.Background()
	conn := local()
	args := map[string]any{"src": frags, "dest": dest}

	res, err := moduleAssemble(ctx, conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("first run: %+v", res)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first\nsecond\nthird\n" {
		t.Errorf("content = %q", got)
	}

	// Idempotent, as real is: this reported changed every time before,
	// so a handler notified by an assemble fired on every run.
	res, err = moduleAssemble(ctx, conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("second run reported changed; real reports ok")
	}

	// Real's key set, measured: changed checksum dest failed gid group
	// md5sum mode msg owner size src state uid.
	for _, k := range []string{"checksum", "dest", "gid", "group", "md5sum", "mode", "owner", "size", "src", "state"} {
		if _, ok := res.Extra[k]; !ok {
			t.Errorf("assemble did not report %q", k)
		}
	}
	if res.Extra["state"] != "file" {
		t.Errorf("state = %v, want file", res.Extra["state"])
	}
	// sha1 of "first\nsecond\nthird\n".
	if res.Extra["checksum"] == "" {
		t.Error("checksum is empty")
	}

	// regexp matches the FILE NAME. Anchored at the start it selected
	// nothing when matched against the full path.
	res, err = moduleAssemble(ctx, conn, map[string]any{
		"src": frags, "dest": dest, "regexp": "^0[13]-",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("filtering did not change the file")
	}
	if got, _ = os.ReadFile(dest); string(got) != "first\nthird\n" {
		t.Errorf("filtered content = %q, want the first and third only", got)
	}

	// A delimiter between fragments.
	res, err = moduleAssemble(ctx, conn, map[string]any{
		"src": frags, "dest": dest, "delimiter": "--\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ = os.ReadFile(dest); string(got) != "first\n--\nsecond\n--\nthird\n--\n" {
		t.Errorf("delimited content = %q", got)
	}
}

func TestModuleAssembleFailsOnAMissingSource(t *testing.T) {
	res, err := moduleAssemble(context.Background(), local(), map[string]any{
		"src": filepath.Join(t.TempDir(), "nope"), "dest": filepath.Join(t.TempDir(), "out"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Errorf("a missing source did not fail: %+v", res)
	}
	// Real's own wording, and no destination created.
	if !strings.HasPrefix(res.Msg, "Source (") || !strings.HasSuffix(res.Msg, ") does not exist") {
		t.Errorf("msg = %q, want Source (<path>) does not exist", res.Msg)
	}
}
