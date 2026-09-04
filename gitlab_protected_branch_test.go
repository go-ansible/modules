package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabProtectedBranchCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/g%2Fp/protected_branches/main -X GET":       {RC: 1, Stderr: "404 Not Found"},
		"glab api projects/g%2Fp/protected_branches -X POST --input -": {RC: 0},
	})
	args := map[string]any{"project": "g/p", "name": "main"}
	res, err := moduleGitlabProtectedBranch(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGitlabProtectedBranchIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/g%2Fp/protected_branches/main -X GET": {RC: 0, Stdout: `{"name":"main","push_access_levels":[{"access_level":40}],"merge_access_levels":[{"access_level":40}],"allow_force_push":false,"code_owner_approval_required":false}`},
	})
	args := map[string]any{"project": "g/p", "name": "main"}
	res, err := moduleGitlabProtectedBranch(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("expected only the binary check and GET call, got %v", conn.Commands)
	}
}

func TestModuleGitlabProtectedBranchAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/g%2Fp/protected_branches/main -X GET":    {RC: 0, Stdout: `{"name":"main"}`},
		"glab api projects/g%2Fp/protected_branches/main -X DELETE": {RC: 0},
	})
	args := map[string]any{"project": "g/p", "name": "main", "state": "absent"}
	res, err := moduleGitlabProtectedBranch(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
