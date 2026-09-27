package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Measured from real ansible-core 2.21.4:
//
//	changed:   msg "2 replacements made", rc 0
//	unchanged: msg "" (empty), rc 0, keys changed/failed/msg/rc
//	one match: msg "1 replacements made" -- NOT singularised
//	missing:   msg "Path <p> does not exist !", rc 257
func TestModuleReplaceResultShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("alpha one\nbeta two\nalpha three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	ctx := context.Background()

	res, err := moduleReplace(ctx, conn, map[string]any{
		"path": path, "regexp": "alpha", "replace": "ALPHA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Msg != "2 replacements made" {
		t.Errorf("changed run: changed=%v msg=%q", res.Changed, res.Msg)
	}
	if res.Extra["rc"] != 0 {
		t.Errorf("rc = %v, want 0", res.Extra["rc"])
	}

	// Run again: nothing left to replace.
	res, err = moduleReplace(ctx, conn, map[string]any{
		"path": path, "regexp": "alpha", "replace": "ALPHA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Msg != "" {
		t.Errorf("unchanged run: changed=%v msg=%q, want false and empty", res.Changed, res.Msg)
	}
	if res.Extra["rc"] != 0 {
		t.Errorf("unchanged rc = %v, want 0", res.Extra["rc"])
	}

	// A single match still says "replacements", plural.
	res, err = moduleReplace(ctx, conn, map[string]any{
		"path": path, "regexp": "one", "replace": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != "1 replacements made" {
		t.Errorf("single match msg = %q, want %q", res.Msg, "1 replacements made")
	}

	missing := filepath.Join(dir, "nope.txt")
	res, err = moduleReplace(ctx, conn, map[string]any{
		"path": missing, "regexp": "x", "replace": "y",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Error("a missing path did not fail")
	}
	if want := "Path " + missing + " does not exist !"; res.Msg != want {
		t.Errorf("msg = %q, want %q", res.Msg, want)
	}
	if res.Extra["rc"] != 257 {
		t.Errorf("rc = %v, want 257", res.Extra["rc"])
	}
}
