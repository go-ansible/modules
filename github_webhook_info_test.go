package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGithubWebhookInfoList(t *testing.T) {
	listCmd := ghCmd("api", "repos/owner/repo/hooks")
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: `[{"id":6206,"active":true,"events":["push"],"config":{"url":"https://example.com/hook","content_type":"json","insecure_ssl":"0"}}]`},
	})
	res, err := moduleGithubWebhookInfo(context.Background(), conn, map[string]any{
		"repository": "owner/repo", "user": "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	hooks, ok := res.Extra["hooks"].([]map[string]any)
	if !ok || len(hooks) != 1 {
		t.Fatalf("hooks = %+v", res.Extra["hooks"])
	}
	if hooks[0]["id"] != 6206 || hooks[0]["has_shared_secret"] != false {
		t.Fatalf("hook = %+v", hooks[0])
	}
}

func TestModuleGithubWebhookInfoFailure(t *testing.T) {
	listCmd := ghCmd("api", "repos/owner/repo/hooks")
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 1, Stderr: "boom"},
	})
	res, err := moduleGithubWebhookInfo(context.Background(), conn, map[string]any{
		"repository": "owner/repo", "user": "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
