package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabIssueCreate(t *testing.T) {
	listCmd := "glab issue list -R group1/project1 --search demo-issue --in title -F json --per-page 100"
	createCmd := "glab issue create -R group1/project1 -t demo-issue --yes"
	viewCmd := "glab issue view 5 -R group1/project1 -F json"
	// the same list command is used both to check for an existing issue
	// (empty) and, after creation, to recover the new issue's IID (glab
	// issue create prints a URL, not JSON) — the SAME probe command must
	// answer differently before/after the action, so this uses seqConn
	// (pacemaker_test_helpers_test.go), not fakeConn.
	conn := newSeqConn(map[string][]remoteexec.Result{
		listCmd: {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"iid":5,"title":"demo-issue","state":"opened"}]`},
		},
		createCmd: {{RC: 0, Stdout: "https://gitlab.example.com/group1/project1/-/issues/5\n"}},
		viewCmd:   {{RC: 0, Stdout: `{"iid":5,"title":"demo-issue","state":"opened"}`}},
	})
	args := map[string]any{"project": "group1/project1", "title": "demo-issue"}
	res, err := moduleGitlabIssue(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	issue := res.Extra["issue"].(gitlabIssueObj)
	if issue.IID != 5 {
		t.Fatalf("issue = %+v", issue)
	}
}

func TestModuleGitlabIssueIdempotent(t *testing.T) {
	listCmd := "glab issue list -R group1/project1 --search demo-issue --in title -F json --per-page 100"
	viewCmd := "glab issue view 5 -R group1/project1 -F json"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: `[{"iid":5,"title":"demo-issue","state":"opened","description":"d"}]`},
		viewCmd: {RC: 0, Stdout: `{"iid":5,"title":"demo-issue","state":"opened","description":"d"}`},
	})
	args := map[string]any{"project": "group1/project1", "title": "demo-issue", "description": "d"}
	res, err := moduleGitlabIssue(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabIssueUpdateLabels(t *testing.T) {
	listCmd := "glab issue list -R group1/project1 --search demo-issue --in title -F json --per-page 100"
	updateCmd := "glab issue update 5 -R group1/project1 -l wontfix -u bug"
	viewCmd := "glab issue view 5 -R group1/project1 -F json"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd:   {RC: 0, Stdout: `[{"iid":5,"title":"demo-issue","state":"opened","labels":["bug"]}]`},
		updateCmd: {RC: 0},
		viewCmd:   {RC: 0, Stdout: `{"iid":5,"title":"demo-issue","state":"opened","labels":["wontfix"]}`},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "demo-issue", "labels": []any{"wontfix"},
	}
	res, err := moduleGitlabIssue(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabIssueAbsentDeletes(t *testing.T) {
	listCmd := "glab issue list -R group1/project1 --search demo-issue --in title -F json --per-page 100"
	deleteCmd := "glab issue delete 5 -R group1/project1"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd:   {RC: 0, Stdout: `[{"iid":5,"title":"demo-issue","state":"opened"}]`},
		deleteCmd: {RC: 0},
	})
	args := map[string]any{"project": "group1/project1", "title": "demo-issue", "state": "absent"}
	res, err := moduleGitlabIssue(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabIssueMultipleMatchesFails(t *testing.T) {
	listCmd := "glab issue list -R group1/project1 --search demo-issue --in title -F json --per-page 100"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: `[{"iid":5,"title":"demo-issue","state":"opened"},{"iid":6,"title":"demo-issue","state":"opened"}]`},
	})
	args := map[string]any{"project": "group1/project1", "title": "demo-issue"}
	res, err := moduleGitlabIssue(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when multiple issues match the title")
	}
}
