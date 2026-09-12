package modules

import (
	"context"
	remoteexec "github.com/go-remoteexec/transport"
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
	for _, name := range []string{"copy", "template", "command", "shell",
		// Read-only modules change nothing either way, so claiming
		// support is simply true — and real Ansible runs them in a check
		// run rather than skipping them.
		"debug", "fail", "stat", "find", "slurp",
		// Writing modules, each with a test below asserting nothing on
		// disk changes.
		"file", "lineinfile", "blockinfile", "replace"} {
		if !SupportsCheckMode(name) {
			t.Errorf("%s should declare check-mode support", name)
		}
	}
	for _, name := range []string{"unarchive", "apt", "get_url", "not_a_module"} {
		if SupportsCheckMode(name) {
			t.Errorf("%s must NOT claim check-mode support until it honours it", name)
		}
	}
	// An FQCN resolves like any other name.
	if !SupportsCheckMode("ansible.builtin.copy") {
		t.Error("an FQCN should resolve to the same entry")
	}
}

// TestCheckModeWritingModulesChangeNothing covers the four modules that
// rewrite a file. Each already decided whether it would change before
// writing, so the dry run reports the same answer and stops short of the
// write — the assertion is that the file on disk is byte-identical
// afterwards.
func TestCheckModeWritingModulesChangeNothing(t *testing.T) {
	ctx := context.Background()
	conn := local()

	const original = "alpha\nbeta\n"
	cases := []struct {
		name string
		fn   func(context.Context, remoteexec.Connection, map[string]any) (Result, error)
		args func(path string) map[string]any
	}{
		{"lineinfile", moduleLineinfile, func(p string) map[string]any {
			return map[string]any{"path": p, "line": "gamma"}
		}},
		{"blockinfile", moduleBlockinfile, func(p string) map[string]any {
			return map[string]any{"path": p, "block": "inserted"}
		}},
		{"replace", moduleReplace, func(p string) map[string]any {
			return map[string]any{"path": p, "regexp": "alpha", "replace": "ALPHA"}
		}},
		{"file mode", moduleFile, func(p string) map[string]any {
			return map[string]any{"path": p, "mode": "0600"}
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.txt")
			if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			args := c.args(path)
			args[CheckModeKey] = true

			res, err := c.fn(ctx, conn, args)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Changed {
				t.Errorf("check mode reported no change, but the file would have been modified")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != original {
				t.Errorf("check mode modified the file: %q, want %q", after, original)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o644 {
				t.Errorf("check mode changed the mode to %o", info.Mode().Perm())
			}
		})
	}
}

// TestCheckModeFileStates covers file's other mutating states, since each
// takes its own branch and the no-op has to hold in all of them.
func TestCheckModeFileStates(t *testing.T) {
	ctx := context.Background()
	conn := local()
	dir := t.TempDir()

	existing := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ name, state, path string }{
		{"directory", "directory", filepath.Join(dir, "newdir")},
		{"touch", "touch", filepath.Join(dir, "newfile")},
		{"absent", "absent", existing},
	} {
		t.Run(c.name, func(t *testing.T) {
			res, err := moduleFile(ctx, conn, map[string]any{
				"path": c.path, "state": c.state, CheckModeKey: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !res.Changed {
				t.Errorf("state=%s in check mode: want changed", c.state)
			}
		})
	}

	// Nothing created, nothing removed.
	if _, err := os.Stat(filepath.Join(dir, "newdir")); !os.IsNotExist(err) {
		t.Error("check mode created the directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "newfile")); !os.IsNotExist(err) {
		t.Error("check mode created the file")
	}
	if _, err := os.Stat(existing); err != nil {
		t.Error("check mode removed the file it was only asked to predict removing")
	}
}
