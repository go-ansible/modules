package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabProjectApprovalsIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/g%2Fp/approvals -X GET": {RC: 0, Stdout: `{"reset_approvals_on_push":true}`},
	})
	args := map[string]any{"project": "g/p", "reset_approvals_on_push": true}
	res, err := moduleGitlabProjectApprovals(context.Background(), conn, args)
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

func TestModuleGitlabProjectApprovalsUpdates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/g%2Fp/approvals -X GET":            {RC: 0, Stdout: `{"reset_approvals_on_push":false}`},
		"glab api projects/g%2Fp/approvals -X POST --input -": {RC: 0, Stdout: `{"reset_approvals_on_push":true}`},
	})
	args := map[string]any{"project": "g/p", "reset_approvals_on_push": true}
	res, err := moduleGitlabProjectApprovals(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	settings := res.Extra["project_approvals"].(map[string]any)
	if settings["reset_approvals_on_push"] != true {
		t.Fatalf("project_approvals = %#v", settings)
	}
}
