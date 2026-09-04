package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGithubIssueOpen(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh issue view 42 -R org/repo --json state": {RC: 0, Stdout: `{"state":"OPEN"}`},
	})
	res, err := moduleGithubIssue(context.Background(), conn, map[string]any{
		"organization": "org", "repo": "repo", "issue": 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Extra["issue_status"] != "open" {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want Changed=true — real github_issue.py reports changed even for this read-only lookup (see moduleGithubIssue's own doc comment)")
	}
}

func TestModuleGithubIssueClosed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh issue view 42 -R org/repo --json state": {RC: 0, Stdout: `{"state":"CLOSED"}`},
	})
	res, err := moduleGithubIssue(context.Background(), conn, map[string]any{
		"organization": "org", "repo": "repo", "issue": 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Extra["issue_status"] != "closed" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubIssueNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh issue view 42 -R org/repo --json state": {RC: 1},
	})
	res, err := moduleGithubIssue(context.Background(), conn, map[string]any{
		"organization": "org", "repo": "repo", "issue": 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubIssueMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGithubIssue(context.Background(), conn, map[string]any{"organization": "org", "repo": "repo"}); err == nil {
		t.Fatal("want error for missing issue")
	}
}
