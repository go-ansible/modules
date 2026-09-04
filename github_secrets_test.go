package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGithubSecretsSetRepo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh secret set K -R org/repo": {RC: 0},
	})
	res, err := moduleGithubSecrets(context.Background(), conn, map[string]any{
		"token": "t", "organization": "org", "repository": "repo", "key": "K", "value": "v",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if conn.Stdins[0] != "v" {
		t.Fatalf("stdin = %q, want secret value piped over stdin, not argv", conn.Stdins[0])
	}
	result, _ := res.Extra["result"].(map[string]any)
	if result["response"] != "Secret created" {
		t.Fatalf("result = %+v", result)
	}
}

func TestModuleGithubSecretsSetOrgRequiresVisibility(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGithubSecrets(context.Background(), conn, map[string]any{
		"token": "t", "organization": "org", "key": "K", "value": "v",
	}); err == nil {
		t.Fatal("want error when state=present, repository unset, and visibility missing")
	}
}

func TestModuleGithubSecretsSetOrg(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh secret set K -o org --visibility all": {RC: 0},
	})
	res, err := moduleGithubSecrets(context.Background(), conn, map[string]any{
		"token": "t", "organization": "org", "key": "K", "value": "v", "visibility": "all",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubSecretsDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh secret delete K -R org/repo": {RC: 0},
	})
	res, err := moduleGithubSecrets(context.Background(), conn, map[string]any{
		"token": "t", "organization": "org", "repository": "repo", "key": "K", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGithubSecretsDeleteNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh secret delete K -R org/repo": {RC: 1, Stderr: "failed to delete secret K: HTTP 404: Not Found"},
	})
	res, err := moduleGithubSecrets(context.Background(), conn, map[string]any{
		"token": "t", "organization": "org", "repository": "repo", "key": "K", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
