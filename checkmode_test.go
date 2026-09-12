package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestCheckModeMakesNoChanges is the property that matters: a dry run
// reports what would happen and touches nothing. Anything less makes
// --check unsafe to trust.
func TestCheckModeMakesNoChanges(t *testing.T) {
	ctx := context.Background()
	conn := local()
	dir := t.TempDir()

	t.Run("copy", func(t *testing.T) {
		dest := filepath.Join(dir, "c.txt")
		res, err := moduleCopy(ctx, conn, map[string]any{
			"content": "hello\n", "dest": dest, CheckModeKey: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Changed {
			t.Error("check mode reported no change for a file that does not exist")
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Error("check mode created the file")
		}

		// Really create it, then check again: no change, still untouched.
		if _, err := moduleCopy(ctx, conn, map[string]any{"content": "hello\n", "dest": dest}); err != nil {
			t.Fatal(err)
		}
		res, err = moduleCopy(ctx, conn, map[string]any{
			"content": "hello\n", "dest": dest, CheckModeKey: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Changed {
			t.Error("check mode reported a change for identical content")
		}
	})

	t.Run("template renders but does not write", func(t *testing.T) {
		src := filepath.Join(dir, "t.j2")
		if err := os.WriteFile(src, []byte("v={{ v }}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(dir, "t.out")
		res, err := moduleTemplate(ctx, conn, map[string]any{
			"src": src, "dest": dest, "_vars": map[string]any{"v": "x"}, CheckModeKey: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Changed {
			t.Error("check mode reported no change for a missing destination")
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Error("check mode wrote the rendered template")
		}
	})

	t.Run("command declines to run", func(t *testing.T) {
		marker := filepath.Join(dir, "ran.txt")
		res, err := moduleCommand(ctx, conn, map[string]any{
			"_raw_params": "touch " + marker, CheckModeKey: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !res.Skipped {
			t.Errorf("command in check mode: %+v, want skipped", res)
		}
		// Real Ansible's own wording.
		if res.Msg != "Command would have run if not in check mode" {
			t.Errorf("msg = %q", res.Msg)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Error("check mode ran the command")
		}
	})
}

// TestSupportsCheckMode guards the default that makes incremental support
// safe: a module nobody has ported is NOT claimed to support check mode,
// so the engine skips it rather than running it for real.
func TestSupportsCheckMode(t *testing.T) {
	for _, name := range []string{"copy", "template", "command", "shell"} {
		if !SupportsCheckMode(name) {
			t.Errorf("%s should declare check-mode support", name)
		}
	}
	for _, name := range []string{"file", "lineinfile", "unarchive", "apt", "not_a_module"} {
		if SupportsCheckMode(name) {
			t.Errorf("%s must NOT claim check-mode support until it honours it", name)
		}
	}
	// An FQCN resolves like any other name.
	if !SupportsCheckMode("ansible.builtin.copy") {
		t.Error("an FQCN should resolve to the same entry")
	}
}
