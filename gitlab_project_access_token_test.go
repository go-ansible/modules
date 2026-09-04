package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabProjectAccessTokenCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/access_tokens?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api projects/g%2Fp/access_tokens -X POST --input -":                {RC: 0, Stdout: `{"id":9,"name":"tok1","token":"secretvalue123"}`},
	})
	args := map[string]any{
		"project":    "g/p",
		"name":       "tok1",
		"scopes":     []any{"api"},
		"expires_at": "2024-12-31",
	}
	res, err := moduleGitlabProjectAccessToken(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	tok := res.Extra["access_token"].(gitlabAccessTokenObj)
	if tok.Token != "secretvalue123" {
		t.Fatalf("token = %q", tok.Token)
	}
}

func TestModuleGitlabProjectAccessTokenRecreateNeverLeavesExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/access_tokens?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"id":9,"name":"tok1","scopes":["api"],"access_level":40,"revoked":false}]`},
	})
	args := map[string]any{
		"project": "g/p",
		"name":    "tok1",
		"scopes":  []any{"api"},
	}
	res, err := moduleGitlabProjectAccessToken(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged (recreate=never)")
	}
	if _, ok := res.Extra["access_token"]; ok {
		t.Fatal("no token value expected when nothing was created/recreated")
	}
}

func TestModuleGitlabProjectAccessTokenAbsentRevokes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/access_tokens?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"id":9,"name":"tok1","revoked":false}]`},
		"glab api projects/g%2Fp/access_tokens/9 -X DELETE":                      {RC: 0},
	})
	args := map[string]any{"project": "g/p", "name": "tok1", "state": "absent"}
	res, err := moduleGitlabProjectAccessToken(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
