package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitConfigInfoNameSingleValue(t *testing.T) {
	base := "git config --system --includes"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get-all " + shellQuote("core.editor"): {RC: 0, Stdout: "vim\n"},
	})
	res, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{"name": "core.editor"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("git_config_info must never report Changed")
	}
	if res.Extra["config_value"] != "vim" {
		t.Fatalf("config_value = %v", res.Extra["config_value"])
	}
	cv := res.Extra["config_values"].(map[string]any)
	values := cv["core.editor"].([]any)
	if len(values) != 1 || values[0] != "vim" {
		t.Fatalf("config_values = %v", cv)
	}
}

func TestModuleGitConfigInfoNameMultiValue(t *testing.T) {
	base := "git config --global --includes"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get-all " + shellQuote("push.pushoption"): {RC: 0, Stdout: "merge_request.create\nmerge_request.draft\n"},
	})
	res, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{
		"name": "push.pushoption", "scope": "global",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["config_value"] != "merge_request.create" {
		t.Fatalf("config_value = %v (want first value)", res.Extra["config_value"])
	}
	values := res.Extra["config_values"].(map[string]any)["push.pushoption"].([]any)
	if len(values) != 2 {
		t.Fatalf("values = %v", values)
	}
}

func TestModuleGitConfigInfoNameUnset(t *testing.T) {
	base := "git config --system --includes"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get-all " + shellQuote("core.editor"): {RC: 1},
	})
	res, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{"name": "core.editor"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["config_value"] != "" {
		t.Fatalf("config_value = %v, want empty", res.Extra["config_value"])
	}
	values := res.Extra["config_values"].(map[string]any)["core.editor"].([]any)
	if len(values) != 0 {
		t.Fatalf("values = %v, want empty (key still present)", values)
	}
}

func TestModuleGitConfigInfoAllValues(t *testing.T) {
	base := "git config --system --includes"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --list": {RC: 0, Stdout: "core.editor=vim\ncolor.ui=auto\npush.pushoption=merge_request.create\npush.pushoption=merge_request.draft\n"},
	})
	res, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["config_value"] != "" {
		t.Fatalf("config_value = %v, want empty when name not set", res.Extra["config_value"])
	}
	cv := res.Extra["config_values"].(map[string]any)
	if v := cv["core.editor"].([]any); len(v) != 1 || v[0] != "vim" {
		t.Fatalf("core.editor = %v", v)
	}
	if v := cv["push.pushoption"].([]any); len(v) != 2 {
		t.Fatalf("push.pushoption = %v", v)
	}
}

func TestModuleGitConfigInfoScopeNotYetSet(t *testing.T) {
	base := "git config --global --includes"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --list": {RC: 128, Stderr: "fatal: unable to read config file '/nonexistent/.gitconfig': No such file or directory"},
	})
	res, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{"scope": "global"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want tolerated (not-yet-existing scope file is not a failure)")
	}
	cv := res.Extra["config_values"].(map[string]any)
	if len(cv) != 0 {
		t.Fatalf("config_values = %v", cv)
	}
}

func TestModuleGitConfigInfoRealError(t *testing.T) {
	base := "git config --system --includes"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --list": {RC: 3, Stderr: "fatal: bad config line 4 in file /etc/gitconfig"},
	})
	res, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a genuine git config error")
	}
}

func TestModuleGitConfigInfoLocalUsesRepoPath(t *testing.T) {
	base := "git -C " + shellQuote("/srv/repo") + " config --local --includes"
	conn := newFakeConn(map[string]remoteexec.Result{
		base + " --get-all " + shellQuote("color.ui"): {RC: 0, Stdout: "auto\n"},
	})
	res, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{
		"name": "color.ui", "scope": "local", "path": "/srv/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["config_value"] != "auto" {
		t.Fatalf("config_value = %v", res.Extra["config_value"])
	}
}

func TestModuleGitConfigInfoValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{"scope": "bogus"}); err == nil {
		t.Fatal("want error for bad scope")
	}
	if _, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{"scope": "local"}); err == nil {
		t.Fatal("want error: path required when scope is local")
	}
	if _, err := moduleGitConfigInfo(context.Background(), conn, map[string]any{"scope": "file"}); err == nil {
		t.Fatal("want error: path required when scope is file")
	}
}
