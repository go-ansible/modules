package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabGroupAccessTokenCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/access_tokens?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api groups/my_group/access_tokens -X POST --input -": {
			RC: 0, Stdout: `{"id":1,"name":"tok1","scopes":["api"],"access_level":40,"expires_at":"2030-01-01","token":"secret"}`,
		},
	})
	args := map[string]any{
		"group": "my_group", "name": "tok1", "scopes": []any{"api"}, "expires_at": "2030-01-01",
	}
	res, err := moduleGitlabGroupAccessToken(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	tok := res.Extra["access_token"].(gitlabAccessTokenObj)
	if tok.Token != "secret" {
		t.Fatalf("token = %+v", tok)
	}
}

func TestModuleGitlabGroupAccessTokenRecreateNeverLeavesAlone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/access_tokens?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"id":1,"name":"tok1","scopes":["api"],"access_level":40,"expires_at":"2030-01-01","revoked":false}]`,
		},
	})
	args := map[string]any{
		"group": "my_group", "name": "tok1", "scopes": []any{"api"}, "expires_at": "2030-01-01",
	}
	res, err := moduleGitlabGroupAccessToken(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if _, ok := res.Extra["access_token"]; ok {
		t.Fatal("want no access_token in Extra when recreate=never leaves the existing token alone")
	}
}

func TestModuleGitlabGroupAccessTokenAbsentRevokes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/access_tokens?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"id":1,"name":"tok1","scopes":["api"],"access_level":40,"revoked":false}]`,
		},
		"glab api groups/my_group/access_tokens/1 -X DELETE": {RC: 0},
	})
	args := map[string]any{"group": "my_group", "name": "tok1", "state": "absent"}
	res, err := moduleGitlabGroupAccessToken(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
