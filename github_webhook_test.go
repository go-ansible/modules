package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGithubWebhookCreate(t *testing.T) {
	listCmd := ghCmd("api", "repos/owner/repo/hooks")
	createArgs := append([]string{"api", "-X", "POST", "repos/owner/repo/hooks", "-f", "name=web"},
		ghWebhookConfigFields(map[string]any{"url": "https://example.com/hook", "events": []any{"push"}})...)
	createCmd := ghCmd(createArgs...)

	conn := newSeqConn(map[string][]remoteexec.Result{
		listCmd:   {{RC: 0, Stdout: "[]"}},
		createCmd: {{RC: 0, Stdout: `{"id":6206}`}},
	})
	res, err := moduleGithubWebhook(context.Background(), conn, map[string]any{
		"repository": "owner/repo", "url": "https://example.com/hook", "events": []any{"push"}, "user": "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["hook_id"] != 6206 {
		t.Fatalf("hook_id = %v", res.Extra["hook_id"])
	}
}

func TestModuleGithubWebhookNoChangeWhenIdentical(t *testing.T) {
	listCmd := ghCmd("api", "repos/owner/repo/hooks")
	existing := `[{"id":6206,"active":true,"events":["push"],"config":{"url":"https://example.com/hook","content_type":"form","insecure_ssl":"0"}}]`
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: existing},
	})
	res, err := moduleGithubWebhook(context.Background(), conn, map[string]any{
		"repository": "owner/repo", "url": "https://example.com/hook", "events": []any{"push"}, "user": "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["hook_id"] != 6206 {
		t.Fatalf("hook_id = %v", res.Extra["hook_id"])
	}
}

func TestModuleGithubWebhookUpdateWhenDifferent(t *testing.T) {
	listCmd := ghCmd("api", "repos/owner/repo/hooks")
	existing := `[{"id":6206,"active":true,"events":["push"],"config":{"url":"https://example.com/hook","content_type":"form","insecure_ssl":"0"}}]`
	editArgs := append([]string{"api", "-X", "PATCH", "repos/owner/repo/hooks/6206", "-f", "name=web"},
		ghWebhookConfigFields(map[string]any{"url": "https://example.com/hook", "events": []any{"push", "pull_request"}})...)
	editCmd := ghCmd(editArgs...)

	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: existing},
		editCmd: {RC: 0},
	})
	res, err := moduleGithubWebhook(context.Background(), conn, map[string]any{
		"repository": "owner/repo", "url": "https://example.com/hook",
		"events": []any{"push", "pull_request"}, "user": "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubWebhookDelete(t *testing.T) {
	listCmd := ghCmd("api", "repos/owner/repo/hooks")
	existing := `[{"id":6206,"active":true,"events":["push"],"config":{"url":"https://example.com/hook"}}]`
	deleteCmd := ghCmd("api", "-X", "DELETE", "repos/owner/repo/hooks/6206")

	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd:   {RC: 0, Stdout: existing},
		deleteCmd: {RC: 0},
	})
	res, err := moduleGithubWebhook(context.Background(), conn, map[string]any{
		"repository": "owner/repo", "url": "https://example.com/hook", "state": "absent", "user": "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubWebhookMissingEvents(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGithubWebhook(context.Background(), conn, map[string]any{
		"repository": "owner/repo", "url": "https://example.com/hook", "user": "u",
	}); err == nil {
		t.Fatal("want error when events is missing for state=present")
	}
}
