package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGithubDeployKeyAdd(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"gh repo deploy-key list --json id,key,title -R owner/repo": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":123,"key":"ssh-rsa AAAAB3 mykey","title":"mykey"}]`},
		},
		"gh repo deploy-key add - -t mykey -R owner/repo": {{RC: 0}},
	})
	res, err := moduleGithubDeployKey(context.Background(), conn, map[string]any{
		"owner": "owner", "repo": "repo", "name": "mykey", "key": "ssh-rsa AAAAB3 mykey",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != 123 {
		t.Fatalf("id = %v", res.Extra["id"])
	}
	if len(conn.Commands) != 3 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleGithubDeployKeyAddAlreadyExists(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"gh repo deploy-key list --json id,key,title -R owner/repo": {
			{RC: 0, Stdout: `[{"id":5,"key":"ssh-rsa AAAAB3 comment","title":"mykey"}]`},
		},
		"gh repo deploy-key add - -t mykey -R owner/repo": {{RC: 1, Stderr: "HTTP 422: Validation Failed"}},
	})
	res, err := moduleGithubDeployKey(context.Background(), conn, map[string]any{
		"owner": "owner", "repo": "repo", "name": "mykey", "key": "ssh-rsa AAAAB3 different-comment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Msg != "Deploy key already exists" {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestModuleGithubDeployKeyDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh repo deploy-key list --json id,key,title -R owner/repo": {RC: 0, Stdout: `[{"id":9,"key":"ssh-rsa AAAAB3 x","title":"mykey"}]`},
		"gh repo deploy-key delete 9 -R owner/repo":                 {RC: 0},
	})
	res, err := moduleGithubDeployKey(context.Background(), conn, map[string]any{
		"owner": "owner", "repo": "repo", "name": "mykey", "key": "ssh-rsa AAAAB3 x", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != 9 {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleGithubDeployKeyDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh repo deploy-key list --json id,key,title -R owner/repo": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleGithubDeployKey(context.Background(), conn, map[string]any{
		"owner": "owner", "repo": "repo", "name": "mykey", "key": "ssh-rsa AAAAB3 x", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Msg != "Deploy key does not exist" {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestModuleGithubDeployKeyMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGithubDeployKey(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
