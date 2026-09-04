package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabProjectCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/myproj -X GET":     {RC: 1, Stderr: "404 Project Not Found"},
		"glab api projects -X POST --input -": {RC: 0, Stdout: `{"id":10,"name":"myproj","path":"myproj"}`},
	})
	args := map[string]any{"name": "myproj"}
	res, err := moduleGitlabProject(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	proj := res.Extra["project"].(map[string]any)
	if proj["id"] != float64(10) {
		t.Fatalf("project = %#v", proj)
	}
}

func TestModuleGitlabProjectIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/myproj -X GET": {RC: 0, Stdout: `{"id":10,"name":"myproj","description":"hello"}`},
	})
	args := map[string]any{"name": "myproj", "description": "hello"}
	res, err := moduleGitlabProject(context.Background(), conn, args)
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

func TestModuleGitlabProjectAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/myproj -X GET": {RC: 0, Stdout: `{"id":10,"name":"myproj"}`},
		"glab api projects/10 -X DELETE":  {RC: 0},
	})
	args := map[string]any{"name": "myproj", "state": "absent"}
	res, err := moduleGitlabProject(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGitlabProjectUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api projects/myproj -X GET":       {RC: 0, Stdout: `{"id":10,"name":"myproj","description":"old"}`},
		"glab api projects/10 -X PUT --input -": {RC: 0, Stdout: `{"id":10,"name":"myproj","description":"new"}`},
	})
	args := map[string]any{"name": "myproj", "description": "new"}
	res, err := moduleGitlabProject(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
