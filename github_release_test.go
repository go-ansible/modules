package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGithubReleaseLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh release view --json tagName -R user/repo": {RC: 0, Stdout: `{"tagName":"v1.0.0"}`},
	})
	res, err := moduleGithubRelease(context.Background(), conn, map[string]any{
		"user": "user", "repo": "repo", "action": "latest_release",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Extra["tag"] != "v1.0.0" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubReleaseLatestNone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh release view --json tagName -R user/repo": {RC: 1},
	})
	res, err := moduleGithubRelease(context.Background(), conn, map[string]any{
		"user": "user", "repo": "repo", "action": "latest_release",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Extra["tag"] != nil {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubReleaseCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh release view v1.0.0 --json tagName -R user/repo": {RC: 1},
		"gh release create v1.0.0 -R user/repo --notes ''":   {RC: 0},
	})
	res, err := moduleGithubRelease(context.Background(), conn, map[string]any{
		"user": "user", "repo": "repo", "action": "create_release", "tag": "v1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed || res.Extra["tag"] != "v1.0.0" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubReleaseCreateAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh release view v1.0.0 --json tagName -R user/repo": {RC: 0, Stdout: `{"tagName":"v1.0.0"}`},
	})
	res, err := moduleGithubRelease(context.Background(), conn, map[string]any{
		"user": "user", "repo": "repo", "action": "create_release", "tag": "v1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Msg != "Release for tag v1.0.0 already exists." {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestModuleGithubReleaseMissingTag(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGithubRelease(context.Background(), conn, map[string]any{
		"user": "user", "repo": "repo", "action": "create_release",
	}); err == nil {
		t.Fatal("want error for missing tag on create_release")
	}
}
