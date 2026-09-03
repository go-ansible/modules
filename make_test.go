package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleMakeBuildsDefaultTarget(t *testing.T) {
	dir := t.TempDir()
	writeSimpleMakefile(t, dir)
	conn := local()
	res, err := moduleMake(context.Background(), conn, map[string]any{"chdir": dir})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	marker := filepath.Join(dir, "built")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("want make to have run: %v", err)
	}
}

func TestModuleMakeIdempotentQ(t *testing.T) {
	dir := t.TempDir()
	writeSimpleMakefile(t, dir)
	conn := local()
	if _, err := moduleMake(context.Background(), conn, map[string]any{"chdir": dir}); err != nil {
		t.Fatal(err)
	}
	res, err := moduleMake(context.Background(), conn, map[string]any{"chdir": dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: target already up to date per `make -q`")
	}
}

func TestModuleMakeTargetAndTargetsMutuallyExclusive(t *testing.T) {
	conn := local()
	_, err := moduleMake(context.Background(), conn, map[string]any{
		"chdir": t.TempDir(), "target": "a", "targets": []any{"b"},
	})
	if err == nil {
		t.Fatal("want error: target and targets are mutually exclusive")
	}
}

func TestModuleMakeMissingChdir(t *testing.T) {
	conn := local()
	_, err := moduleMake(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing chdir")
	}
}

func TestModuleMakeParams(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte("built:\n\ttest \"$(FOO)\" = bar && touch built\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleMake(context.Background(), conn, map[string]any{
		"chdir":  dir,
		"params": map[string]any{"FOO": "bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	command := res.Extra["command"].(string)
	if command != "make FOO=bar" {
		t.Fatalf("command = %q", command)
	}
}

func writeSimpleMakefile(t *testing.T, dir string) {
	t.Helper()
	// The target is itself the file `make -q` checks for freshness, so a
	// second run against an already-built tree is genuinely up to date.
	content := "built:\n\ttouch built\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
