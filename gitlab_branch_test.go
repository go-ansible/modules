package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabBranchCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/group1%2Fproject1/repository/branches/feature1 -X GET": {RC: 1, Stderr: "404 Not Found"},
		"glab api projects/group1%2Fproject1/repository/branches -X POST --input -": {
			RC: 0, Stdout: `{"name":"feature1","protected":false,"default":false}`,
		},
	})
	args := map[string]any{"project": "group1/project1", "branch": "feature1", "ref_branch": "main"}
	res, err := moduleGitlabBranch(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	branch := res.Extra["branch"].(gitlabBranchObj)
	if branch.Name != "feature1" {
		t.Fatalf("branch = %+v", branch)
	}
}

func TestModuleGitlabBranchIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/group1%2Fproject1/repository/branches/feature1 -X GET": {
			RC: 0, Stdout: `{"name":"feature1","protected":false,"default":false}`,
		},
	})
	args := map[string]any{"project": "group1/project1", "branch": "feature1", "ref_branch": "main"}
	res, err := moduleGitlabBranch(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabBranchAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/group1%2Fproject1/repository/branches/feature1 -X GET": {
			RC: 0, Stdout: `{"name":"feature1"}`,
		},
		"glab api projects/group1%2Fproject1/repository/branches/feature1 -X DELETE": {RC: 0},
	})
	args := map[string]any{"project": "group1/project1", "branch": "feature1", "state": "absent"}
	res, err := moduleGitlabBranch(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabBranchAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/group1%2Fproject1/repository/branches/feature1 -X GET": {RC: 1, Stderr: "404 Not Found"},
	})
	args := map[string]any{"project": "group1/project1", "branch": "feature1", "state": "absent"}
	res, err := moduleGitlabBranch(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabBranchMissingRefBranch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/group1%2Fproject1/repository/branches/feature1 -X GET": {RC: 1, Stderr: "404 Not Found"},
	})
	args := map[string]any{"project": "group1/project1", "branch": "feature1"}
	if _, err := moduleGitlabBranch(context.Background(), conn, args); err == nil {
		t.Fatal("want error: ref_branch is required to create a branch")
	}
}
