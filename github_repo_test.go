package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const ghRepoViewFields = "gh repo view org/repo --json name,owner,description,isPrivate,visibility,url,sshUrl,createdAt,updatedAt,defaultBranchRef"

func TestModuleGithubRepoCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		ghRepoViewFields: {
			{RC: 1},
			{RC: 0, Stdout: `{"name":"repo","description":"hello","isPrivate":false}`},
		},
		"gh repo create org/repo --public --description hello": {{RC: 0}},
	})
	res, err := moduleGithubRepo(context.Background(), conn, map[string]any{
		"name": "repo", "organization": "org", "description": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	repo, ok := res.Extra["repo"].(map[string]any)
	if !ok || repo["name"] != "repo" {
		t.Fatalf("repo = %+v", res.Extra["repo"])
	}
}

func TestModuleGithubRepoEditExisting(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		ghRepoViewFields: {
			{RC: 0, Stdout: `{"name":"repo","description":"old","isPrivate":false}`},
			{RC: 0, Stdout: `{"name":"repo","description":"new","isPrivate":true}`},
		},
		"gh repo edit org/repo --visibility private --accept-visibility-change-consequences --description new": {{RC: 0}},
	})
	res, err := moduleGithubRepo(context.Background(), conn, map[string]any{
		"name": "repo", "organization": "org", "private": true, "description": "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubRepoNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		ghRepoViewFields: {RC: 0, Stdout: `{"name":"repo","description":"same","isPrivate":false}`},
	})
	res, err := moduleGithubRepo(context.Background(), conn, map[string]any{
		"name": "repo", "organization": "org", "private": false, "description": "same",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubRepoDeleteExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh repo view org/repo --json name": {RC: 0},
		"gh repo delete org/repo --yes":     {RC: 0},
	})
	res, err := moduleGithubRepo(context.Background(), conn, map[string]any{
		"name": "repo", "organization": "org", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubRepoDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gh repo view org/repo --json name": {RC: 1},
	})
	res, err := moduleGithubRepo(context.Background(), conn, map[string]any{
		"name": "repo", "organization": "org", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
