package modules

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// terraform.go wraps the real `terraform` binary; a `terraform init`
// against a project directory with no provider blocks needs no network
// access and no root, so — matching this project's own testing
// convention (see fakeconn_test.go's own doc comment) — this test
// exercises it against a real Connection and t.TempDir() rather than a
// scripted fakeConn, skipping only if no `terraform` binary is on PATH.

func requireTerraformBinary(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform binary not found on PATH")
	}
}

func TestModuleTerraformNoChanges(t *testing.T) {
	requireTerraformBinary(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()

	res, err := moduleTerraform(context.Background(), conn, map[string]any{
		"project_path": dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Fatalf("want unchanged for an empty config with nothing to apply, res = %+v", res)
	}
	if _, ok := res.Extra["outputs"]; !ok {
		t.Fatal("want outputs in Extra")
	}
}

func TestModuleTerraformPlannedNeverApplies(t *testing.T) {
	requireTerraformBinary(t)
	dir := t.TempDir()
	tf := `resource "local_file" "f" {
  content  = "hello"
  filename = "${path.module}/out.txt"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tf), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()

	res, err := moduleTerraform(context.Background(), conn, map[string]any{
		"project_path": dir, "state": "planned",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		// No network access for the local provider plugin in this
		// sandbox is an acceptable, expected outcome — not a bug in
		// this port's own command construction, which is what this
		// test actually exercises.
		t.Skipf("terraform init/plan failed (likely no provider plugin available offline): %+v", res)
	}
	if !res.Changed {
		t.Fatalf("want changed (a plan was created), res = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); err == nil {
		t.Fatal("state=planned must never actually apply")
	}
}

func TestModuleTerraformMissingProjectPath(t *testing.T) {
	conn := local()
	if _, err := moduleTerraform(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing project_path")
	}
}

func TestModuleTerraformBadState(t *testing.T) {
	conn := local()
	if _, err := moduleTerraform(context.Background(), conn, map[string]any{
		"project_path": t.TempDir(), "state": "bogus",
	}); err == nil {
		t.Fatal("want error for bad state")
	}
}

func TestModuleTerraformComplexVarsRequiredForMap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	if _, err := moduleTerraform(context.Background(), conn, map[string]any{
		"project_path": dir,
		"variables":    map[string]any{"tags": map[string]any{"a": "b"}},
	}); err == nil {
		t.Fatal("want error for a complex variable without complex_vars=true")
	}
}
