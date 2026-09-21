package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestTemplateFileUsesJinjaStringEscapes pins the semantics a .j2 file
// is rendered with, which are NOT the ones a playbook's own expressions
// use. Measured against real ansible-core 2.21.4 with one expression in
// two places: `{{ "x\ny" | length }}` is 4 inline and 3 in a file.
func TestTemplateFileUsesJinjaStringEscapes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "t.j2")
	dest := filepath.Join(dir, "out")
	if err := os.WriteFile(src, []byte(`A={{ "x\ny" | length }}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := moduleTemplate(context.Background(), local(), map[string]any{
		"src": src, "dest": dest,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	// 3, not 4: in a FILE the \n is an escape, as plain Jinja2 reads it.
	if string(got) != "A=3" {
		t.Errorf("rendered %q, real ansible-core renders %q", got, "A=3")
	}
}

// And a Windows path in a .j2 fails, as it does in real Ansible — the
// same text inline is an ordinary string.
func TestTemplateFileRejectsAnInvalidEscape(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "t.j2")
	if err := os.WriteFile(src, []byte(`{{ "C:\Users" }}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := moduleTemplate(context.Background(), local(), map[string]any{
		"src": src, "dest": filepath.Join(dir, "out"),
	}); err == nil {
		t.Error("a .j2 with an invalid escape must fail, as it does in real Ansible")
	}
}
