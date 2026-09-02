package modules

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo creates a real local git repository at dir with one
// commit on branch "main", for use as a `git` module source — a real
// git binary, not a fake, since git is a reasonable baseline dependency
// and this proves the module's actual clone/fetch/checkout commands.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "--quiet", "-m", "initial")
}

func TestModuleGitClone(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin")
	initGitRepo(t, origin)
	dest := filepath.Join(t.TempDir(), "clone")

	conn := local()
	res, err := moduleGit(context.Background(), conn, map[string]any{"repo": origin, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed on first clone")
	}
	if _, err := os.Stat(filepath.Join(dest, "README")); err != nil {
		t.Fatal(err)
	}
}

func TestModuleGitUpdateNoChange(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin")
	initGitRepo(t, origin)
	dest := filepath.Join(t.TempDir(), "clone")

	conn := local()
	if _, err := moduleGit(context.Background(), conn, map[string]any{"repo": origin, "dest": dest}); err != nil {
		t.Fatal(err)
	}
	res, err := moduleGit(context.Background(), conn, map[string]any{"repo": origin, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: nothing new upstream")
	}
}

func TestModuleGitMissingArgs(t *testing.T) {
	conn := local()
	if _, err := moduleGit(context.Background(), conn, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error for missing repo")
	}
	if _, err := moduleGit(context.Background(), conn, map[string]any{"repo": "x"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}
