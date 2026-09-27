package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func local() *remoteexec.Local { return remoteexec.NewLocal() }

func TestModuleCommand(t *testing.T) {
	conn := local()
	res, err := moduleCommand(context.Background(), conn, map[string]any{"cmd": "echo hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["stdout"] != "hello\n" {
		t.Fatalf("stdout = %q", res.Extra["stdout"])
	}
}

func TestModuleCommandNoShellInterpretation(t *testing.T) {
	// command must NOT interpret "|" as a pipe: it's just an argv token.
	conn := local()
	res, err := moduleCommand(context.Background(), conn, map[string]any{"cmd": "echo a | b"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["stdout"] != "a | b\n" {
		t.Fatalf("stdout = %q, want literal pipe character passed through as an argument", res.Extra["stdout"])
	}
}

func TestModuleCommandNonZero(t *testing.T) {
	conn := local()
	res, err := moduleCommand(context.Background(), conn, map[string]any{"cmd": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-zero exit")
	}
}

func TestModuleCommandCreatesSkips(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCommand(context.Background(), conn, map[string]any{"cmd": "echo should-not-run", "creates": marker})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want skipped (unchanged) when creates path exists")
	}
}

func TestModuleCommandRemovesSkips(t *testing.T) {
	conn := local()
	res, err := moduleCommand(context.Background(), conn, map[string]any{
		"cmd": "echo should-not-run", "removes": filepath.Join(t.TempDir(), "absent"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want skipped (unchanged) when removes path is already absent")
	}
}

func TestModuleCommandArgv(t *testing.T) {
	conn := local()
	res, err := moduleCommand(context.Background(), conn, map[string]any{"argv": []any{"echo", "a b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["stdout"] != "a b c\n" {
		t.Fatalf("stdout = %q", res.Extra["stdout"])
	}
}

func TestModuleCommandChdir(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleCommand(context.Background(), conn, map[string]any{"cmd": "pwd", "chdir": dir})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Extra["stdout"].(string)
	// macOS /tmp is a symlink to /private/tmp; resolve both sides.
	wantDir, _ := filepath.EvalSymlinks(dir)
	gotDir, _ := filepath.EvalSymlinks(trimNL(got))
	if gotDir != wantDir {
		t.Fatalf("pwd = %q, want %q", got, dir)
	}
}

func trimNL(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}

func TestModuleCommandMissingArg(t *testing.T) {
	conn := local()
	if _, err := moduleCommand(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing cmd/argv")
	}
}

func TestModuleShell(t *testing.T) {
	conn := local()
	res, err := moduleShell(context.Background(), conn, map[string]any{"cmd": "echo a | tr a b"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["stdout"] != "b\n" {
		t.Fatalf("stdout = %q, want the pipe to have actually run", res.Extra["stdout"])
	}
}

func TestModuleShellMissingArg(t *testing.T) {
	conn := local()
	if _, err := moduleShell(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing cmd")
	}
}

func TestModuleShellChdir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(f, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleShell(context.Background(), conn, map[string]any{"cmd": "ls", "chdir": dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["stdout"] != "x.txt\n" {
		t.Fatalf("stdout = %q", res.Extra["stdout"])
	}
}

func TestTokenize(t *testing.T) {
	got := tokenize(`echo "a b" c 'd e'`)
	want := []string{"echo", "a b", "c", "d e"}
	if len(got) != len(want) {
		t.Fatalf("tokenize = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokenize[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// chdir must behave like the process-level chdir real performs, not
// like a shell's `cd`: a plain `cd` keeps the LOGICAL path, so a
// following `pwd` reports the symlink it was given rather than the
// directory it landed in.
//
// Measured against real on macOS, where /tmp is a symlink:
// `shell: chdir=/tmp pwd` prints /private/tmp. This builds its own
// symlink so the same property is checked wherever the tests run.
func TestChdirLandsOnThePhysicalDirectory(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(real, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(real, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	for _, mod := range []struct {
		name string
		fn   func(context.Context, remoteexec.Connection, map[string]any) (Result, error)
	}{
		{"command", moduleCommand},
		{"shell", moduleShell},
	} {
		t.Run(mod.name, func(t *testing.T) {
			res, err := mod.fn(context.Background(), local(), map[string]any{
				"cmd": "pwd", "chdir": link,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(res.Extra["stdout"].(string))
			if got != target {
				t.Errorf("pwd = %q, want the physical directory %q (a plain `cd` would report %q)",
					got, target, link)
			}
		})
	}
}
