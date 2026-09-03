package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestGitConfigBaseCmd(t *testing.T) {
	cases := []struct {
		scope, repo, file, want string
	}{
		{"system", "", "", "git config --system"},
		{"global", "", "", "git config --global"},
		{"local", "/srv/repo", "", "git -C " + shellQuote("/srv/repo") + " config --local"},
		{"file", "", "/etc/foo.gitconfig", "git config --file " + shellQuote("/etc/foo.gitconfig")},
	}
	for _, c := range cases {
		if got := gitConfigBaseCmd(c.scope, c.repo, c.file); got != c.want {
			t.Errorf("gitConfigBaseCmd(%q,%q,%q) = %q, want %q", c.scope, c.repo, c.file, got, c.want)
		}
	}
}

func TestModuleGitConfigPresentNew(t *testing.T) {
	base := "git config --global"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get " + shellQuote("user.email") + " 2>/dev/null":                      {RC: 1},
		base + " --replace-all " + shellQuote("user.email") + " " + shellQuote("a@b.com"): {RC: 0},
	})
	res, err := moduleGitConfig(context.Background(), conn, map[string]any{
		"name": "user.email", "value": "a@b.com", "scope": "global",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGitConfigPresentAlready(t *testing.T) {
	base := "git config --global"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get " + shellQuote("user.email") + " 2>/dev/null": {RC: 0, Stdout: "a@b.com\n"},
	})
	res, err := moduleGitConfig(context.Background(), conn, map[string]any{
		"name": "user.email", "value": "a@b.com", "scope": "global",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleGitConfigAddMode(t *testing.T) {
	base := "git config --global"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get-all " + shellQuote("push.pushoption") + " 2>/dev/null":                      {RC: 0, Stdout: "merge_request.create\n"},
		base + " --add " + shellQuote("push.pushoption") + " " + shellQuote("merge_request.draft"): {RC: 0},
	})
	res, err := moduleGitConfig(context.Background(), conn, map[string]any{
		"name": "push.pushoption", "value": "merge_request.draft", "scope": "global", "add_mode": "add",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}

	// Same value already present: no --add call, unchanged.
	conn2 := newFakeConn(map[string]remoteexec.Result{
		base + " --get-all " + shellQuote("push.pushoption") + " 2>/dev/null": {RC: 0, Stdout: "merge_request.create\n"},
	})
	res2, err := moduleGitConfig(context.Background(), conn2, map[string]any{
		"name": "push.pushoption", "value": "merge_request.create", "scope": "global", "add_mode": "add",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleGitConfigAbsent(t *testing.T) {
	base := "git config --global"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get " + shellQuote("alias.ci") + " 2>/dev/null": {RC: 0, Stdout: "commit\n"},
		base + " --unset-all " + shellQuote("alias.ci"):            {RC: 0},
	})
	res, err := moduleGitConfig(context.Background(), conn, map[string]any{
		"name": "alias.ci", "scope": "global", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGitConfigAbsentAlready(t *testing.T) {
	base := "git config --global"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get " + shellQuote("alias.ci") + " 2>/dev/null": {RC: 1},
	})
	res, err := moduleGitConfig(context.Background(), conn, map[string]any{
		"name": "alias.ci", "scope": "global", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleGitConfigLocalRequiresRepo(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGitConfig(context.Background(), conn, map[string]any{
		"name": "user.email", "value": "a@b.com", "scope": "local",
	}); err == nil {
		t.Fatal("want error for local scope without repo")
	}
}

func TestModuleGitConfigFileRequiresFile(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGitConfig(context.Background(), conn, map[string]any{
		"name": "user.email", "value": "a@b.com", "scope": "file",
	}); err == nil {
		t.Fatal("want error for file scope without file")
	}
}

func TestModuleGitConfigValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGitConfig(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleGitConfig(context.Background(), conn, map[string]any{"name": "x", "scope": "bogus"}); err == nil {
		t.Fatal("want error for invalid scope")
	}
	if _, err := moduleGitConfig(context.Background(), conn, map[string]any{"name": "x", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
	if _, err := moduleGitConfig(context.Background(), conn, map[string]any{"name": "x", "value": "y", "add_mode": "bogus"}); err == nil {
		t.Fatal("want error for invalid add_mode")
	}
	if _, err := moduleGitConfig(context.Background(), conn, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error for missing value when state=present")
	}
}

func TestModuleGitConfigLocalScopeUsesRepo(t *testing.T) {
	base := "git -C " + shellQuote("/srv/repo") + " config --local"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get " + shellQuote("user.email") + " 2>/dev/null":                        {RC: 1},
		base + " --replace-all " + shellQuote("user.email") + " " + shellQuote("root@host"): {RC: 0},
	})
	res, err := moduleGitConfig(context.Background(), conn, map[string]any{
		"name": "user.email", "value": "root@host", "scope": "local", "repo": "/srv/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
