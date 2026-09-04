package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabUserCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'users?username=bob' -X GET": {RC: 0, Stdout: "[]"},
		"glab api users -X POST --input -":     {RC: 0, Stdout: `{"id":3,"username":"bob","name":"Bob","email":"bob@example.com","state":"active"}`},
	})
	args := map[string]any{"username": "bob", "name": "Bob", "email": "bob@example.com"}
	res, err := moduleGitlabUser(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	u := res.Extra["user"].(gitlabUserObj)
	if u.Username != "bob" {
		t.Fatalf("user = %#v", u)
	}
}

func TestModuleGitlabUserAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'users?username=bob' -X GET": {RC: 0, Stdout: "[]"},
	})
	args := map[string]any{"username": "bob", "state": "absent"}
	res, err := moduleGitlabUser(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleGitlabUserBlock(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'users?username=bob' -X GET": {RC: 0, Stdout: `[{"id":3,"username":"bob","state":"active"}]`},
		"glab api users/3/block -X POST":       {RC: 0},
	})
	args := map[string]any{"username": "bob", "state": "blocked"}
	res, err := moduleGitlabUser(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGitlabUserBlockAlreadyBlocked(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'users?username=bob' -X GET": {RC: 0, Stdout: `[{"id":3,"username":"bob","state":"blocked"}]`},
	})
	args := map[string]any{"username": "bob", "state": "blocked"}
	res, err := moduleGitlabUser(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}
