package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// deploy_helper is pure filesystem operations with no root or external
// tool dependency, so — matching this project's own testing convention
// (see fakeconn_test.go's own doc comment) — this test exercises it
// against a real Connection (remoteexec.NewLocal()) and t.TempDir(),
// rather than a scripted fakeConn.

func TestModuleDeployHelperPresentCreatesLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	conn := local()

	res, err := moduleDeployHelper(context.Background(), conn, map[string]any{
		"path": root, "release": "20240101000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	for _, dir := range []string{root, filepath.Join(root, "releases"), filepath.Join(root, "shared")} {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Fatalf("%s should be a directory: %v", dir, err)
		}
	}
	facts, ok := res.Facts["deploy_helper"].(map[string]any)
	if !ok {
		t.Fatalf("Facts[deploy_helper] = %#v", res.Facts["deploy_helper"])
	}
	if facts["new_release"] != "20240101000000" {
		t.Fatalf("new_release = %v", facts["new_release"])
	}
}

func TestModuleDeployHelperPresentIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	conn := local()
	args := map[string]any{"path": root, "release": "20240101000000"}

	if _, err := moduleDeployHelper(context.Background(), conn, args); err != nil {
		t.Fatal(err)
	}
	res, err := moduleDeployHelper(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged on second present run")
	}
}

func TestModuleDeployHelperFinalize(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	conn := local()

	if _, err := moduleDeployHelper(context.Background(), conn, map[string]any{
		"path": root, "release": "20240101000000",
	}); err != nil {
		t.Fatal(err)
	}
	releaseDir := filepath.Join(root, "releases", "20240101000000")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "app.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := moduleDeployHelper(context.Background(), conn, map[string]any{
		"path": root, "release": "20240101000000", "state": "finalize",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	current := filepath.Join(root, "current")
	target, err := os.Readlink(current)
	if err != nil {
		t.Fatalf("current symlink missing: %v", err)
	}
	if filepath.Base(target) != "20240101000000" {
		t.Fatalf("current -> %s, want release 20240101000000", target)
	}
}

func TestModuleDeployHelperFinalizeRequiresRelease(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	conn := local()
	if _, err := moduleDeployHelper(context.Background(), conn, map[string]any{
		"path": root, "state": "finalize",
	}); err == nil {
		t.Fatal("want error for finalize without release")
	}
}

func TestModuleDeployHelperCleanTrimsOldReleases(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	releasesDir := filepath.Join(root, "releases")
	if err := os.MkdirAll(releasesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"r1", "r2", "r3", "r4"} {
		if err := os.MkdirAll(filepath.Join(releasesDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	conn := local()
	res, err := moduleDeployHelper(context.Background(), conn, map[string]any{
		"path": root, "state": "clean", "keep_releases": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	entries, err := os.ReadDir(releasesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("releases remaining = %d, want 2", len(entries))
	}
}

func TestModuleDeployHelperAbsentDeletesEverything(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	conn := local()
	if _, err := moduleDeployHelper(context.Background(), conn, map[string]any{
		"path": root, "release": "20240101000000",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := moduleDeployHelper(context.Background(), conn, map[string]any{
		"path": root, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("want %s removed, stat err = %v", root, err)
	}
}

func TestModuleDeployHelperMissingPath(t *testing.T) {
	conn := local()
	if _, err := moduleDeployHelper(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
}
