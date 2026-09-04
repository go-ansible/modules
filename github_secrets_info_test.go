package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGithubSecretsInfoRepo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh secret list --json name,updatedAt -R org/repo": {
			RC: 0, Stdout: `[{"name":"A","updatedAt":"2024-01-01T00:00:00Z"}]`,
		},
	})
	res, err := moduleGithubSecretsInfo(context.Background(), conn, map[string]any{
		"token": "t", "organization": "org", "repository": "repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	secrets, ok := res.Extra["secrets"].([]map[string]any)
	if !ok || len(secrets) != 1 || secrets[0]["name"] != "A" {
		t.Fatalf("secrets = %+v", res.Extra["secrets"])
	}
}

func TestModuleGithubSecretsInfoOrg(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh secret list --json name,updatedAt -o org": {RC: 0, Stdout: `[]`},
	})
	res, err := moduleGithubSecretsInfo(context.Background(), conn, map[string]any{
		"token": "t", "organization": "org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	secrets, ok := res.Extra["secrets"].([]map[string]any)
	if !ok || len(secrets) != 0 {
		t.Fatalf("secrets = %+v", res.Extra["secrets"])
	}
}

func TestModuleGithubSecretsInfoFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"GH_TOKEN=t gh secret list --json name,updatedAt -o org": {RC: 1, Stderr: "boom"},
	})
	res, err := moduleGithubSecretsInfo(context.Background(), conn, map[string]any{
		"token": "t", "organization": "org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
