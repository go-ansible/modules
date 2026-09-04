package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabMergeRequestCreate(t *testing.T) {
	listCmd := "glab mr list -R group1/project1 --all -F json --per-page 100"
	createCmd := "glab mr create -R group1/project1 -s feature1 -b main -t demo-mr --yes"
	viewCmd := "glab mr view 9 -R group1/project1 -F json"
	// same reasoning as gitlab_issue_test.go's own TestModuleGitlabIssueCreate:
	// the list command is re-used to recover the new MR's IID after
	// `glab mr create` (which prints a URL, not JSON), so it must answer
	// differently before/after — seqConn, not fakeConn.
	conn := newSeqConn(map[string][]remoteexec.Result{
		listCmd: {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"iid":9,"title":"demo-mr","state":"opened","source_branch":"feature1","target_branch":"main"}]`},
		},
		createCmd: {{RC: 0, Stdout: "https://gitlab.example.com/group1/project1/-/merge_requests/9\n"}},
		viewCmd:   {{RC: 0, Stdout: `{"iid":9,"title":"demo-mr","state":"opened","source_branch":"feature1","target_branch":"main"}`}},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "demo-mr", "source_branch": "feature1", "target_branch": "main",
	}
	res, err := moduleGitlabMergeRequest(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	mr := res.Extra["mr"].(gitlabMRObj)
	if mr.IID != 9 {
		t.Fatalf("mr = %+v", mr)
	}
}

func TestModuleGitlabMergeRequestIdempotent(t *testing.T) {
	listCmd := "glab mr list -R group1/project1 --all -F json --per-page 100"
	viewCmd := "glab mr view 9 -R group1/project1 -F json"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: `[{"iid":9,"title":"demo-mr","state":"opened","source_branch":"feature1","target_branch":"main"}]`},
		viewCmd: {RC: 0, Stdout: `{"iid":9,"title":"demo-mr","state":"opened","source_branch":"feature1","target_branch":"main"}`},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "demo-mr", "source_branch": "feature1", "target_branch": "main",
	}
	res, err := moduleGitlabMergeRequest(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabMergeRequestAbsentDeletes(t *testing.T) {
	listCmd := "glab mr list -R group1/project1 --all -F json --per-page 100"
	deleteCmd := "glab mr delete 9 -R group1/project1"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd:   {RC: 0, Stdout: `[{"iid":9,"title":"demo-mr","state":"opened","source_branch":"feature1","target_branch":"main"}]`},
		deleteCmd: {RC: 0},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "demo-mr", "source_branch": "feature1", "target_branch": "main",
		"state": "absent",
	}
	res, err := moduleGitlabMergeRequest(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabMergeRequestMultipleMatchesFails(t *testing.T) {
	listCmd := "glab mr list -R group1/project1 --all -F json --per-page 100"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: `[
			{"iid":9,"title":"demo-mr","state":"opened","source_branch":"feature1","target_branch":"main"},
			{"iid":10,"title":"demo-mr","state":"opened","source_branch":"feature1","target_branch":"main"}
		]`},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "demo-mr", "source_branch": "feature1", "target_branch": "main",
	}
	res, err := moduleGitlabMergeRequest(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when multiple merge requests match")
	}
}
