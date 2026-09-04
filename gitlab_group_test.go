package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabGroupCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api groups/my_group -X GET": {RC: 1, Stderr: "404 Not Found"},
		"glab api groups -X POST --input -": {
			RC: 0, Stdout: `{"id":1,"name":"my_group","path":"my_group","full_path":"my_group"}`,
		},
	})
	args := map[string]any{"name": "my_group"}
	res, err := moduleGitlabGroup(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api groups/my_group -X GET": {
			RC: 0, Stdout: `{"id":1,"name":"my_group","path":"my_group","full_path":"my_group","visibility":"private"}`,
		},
	})
	args := map[string]any{"name": "my_group", "visibility": "private"}
	res, err := moduleGitlabGroup(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupUpdatesChangedField(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api groups/my_group -X GET": {
			RC: 0, Stdout: `{"id":1,"name":"my_group","path":"my_group","full_path":"my_group","visibility":"private"}`,
		},
		"glab api groups/my_group -X PUT --input -": {
			RC: 0, Stdout: `{"id":1,"name":"my_group","path":"my_group","full_path":"my_group","visibility":"public"}`,
		},
	})
	args := map[string]any{"name": "my_group", "visibility": "public"}
	res, err := moduleGitlabGroup(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api groups/my_group -X GET": {
			RC: 0, Stdout: `{"id":1,"name":"my_group","path":"my_group","full_path":"my_group"}`,
		},
		"glab api 'groups/my_group/projects?per_page=1' -X GET": {RC: 0, Stdout: "[]"},
		"glab api groups/my_group -X DELETE":                    {RC: 0},
	})
	args := map[string]any{"name": "my_group", "state": "absent"}
	res, err := moduleGitlabGroup(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupAbsentRefusesWithoutForceDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api groups/my_group -X GET": {
			RC: 0, Stdout: `{"id":1,"name":"my_group","path":"my_group","full_path":"my_group"}`,
		},
		"glab api 'groups/my_group/projects?per_page=1' -X GET": {RC: 0, Stdout: `[{"id":1}]`},
	})
	args := map[string]any{"name": "my_group", "state": "absent"}
	res, err := moduleGitlabGroup(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: group still has projects and force_delete is not set")
	}
}
