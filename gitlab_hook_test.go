package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabHookCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/group1%2Fproject1/hooks?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api projects/group1%2Fproject1/hooks -X POST --input -": {
			RC: 0, Stdout: `{"id":1,"url":"https://ci.example.com/hook","push_events":true}`,
		},
	})
	args := map[string]any{
		"project": "group1/project1", "hook_url": "https://ci.example.com/hook",
	}
	res, err := moduleGitlabHook(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabHookIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/group1%2Fproject1/hooks?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"id":1,"url":"https://ci.example.com/hook","push_events":true}]`,
		},
	})
	args := map[string]any{
		"project": "group1/project1", "hook_url": "https://ci.example.com/hook",
	}
	res, err := moduleGitlabHook(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabHookTokenAlwaysChanges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/group1%2Fproject1/hooks?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"id":1,"url":"https://ci.example.com/hook","push_events":true}]`,
		},
		"glab api projects/group1%2Fproject1/hooks/1 -X PUT --input -": {RC: 0},
	})
	args := map[string]any{
		"project": "group1/project1", "hook_url": "https://ci.example.com/hook", "token": "secret-token",
	}
	res, err := moduleGitlabHook(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabHookAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/group1%2Fproject1/hooks?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"id":1,"url":"https://ci.example.com/hook","push_events":true}]`,
		},
		"glab api projects/group1%2Fproject1/hooks/1 -X DELETE": {RC: 0},
	})
	args := map[string]any{
		"project": "group1/project1", "hook_url": "https://ci.example.com/hook", "state": "absent",
	}
	res, err := moduleGitlabHook(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
