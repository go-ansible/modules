package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabDeployKeyCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab deploy-key list -R group1/project1 -F json --per-page 100": {RC: 0, Stdout: "[]"},
		"glab deploy-key add -R group1/project1 -t CI --can-push -":      {RC: 0},
	})
	// after creation, moduleGitlabDeployKey re-lists to recover the created object
	conn.on["glab deploy-key list -R group1/project1 -F json --per-page 100"] = remoteexec.Result{RC: 0, Stdout: "[]"}
	args := map[string]any{
		"project": "group1/project1", "title": "CI", "key": "ssh-rsa AAAA...", "can_push": true,
	}
	res, err := moduleGitlabDeployKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "glab deploy-key add -R group1/project1 -t CI --can-push -" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
	// the key content must be piped over stdin, never on argv
	if len(conn.Stdins) == 0 {
		t.Fatal("want key content piped over stdin")
	}
}

func TestModuleGitlabDeployKeyIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab deploy-key list -R group1/project1 -F json --per-page 100": {
			RC: 0, Stdout: `[{"id":1,"title":"CI","key":"ssh-rsa AAAA...","can_push":true}]`,
		},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "CI", "key": "ssh-rsa AAAA...", "can_push": true,
	}
	res, err := moduleGitlabDeployKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabDeployKeyCanPushOnlyChangeUsesAPIFallback(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab deploy-key list -R group1/project1 -F json --per-page 100": {
			RC: 0, Stdout: `[{"id":1,"title":"CI","key":"ssh-rsa AAAA...","can_push":false}]`,
		},
		"glab api projects/group1%2Fproject1/deploy_keys/1 -X PUT --input -": {
			RC: 0, Stdout: `{"id":1,"title":"CI","key":"ssh-rsa AAAA...","can_push":true}`,
		},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "CI", "key": "ssh-rsa AAAA...", "can_push": true,
	}
	res, err := moduleGitlabDeployKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabDeployKeyKeyContentChangeDeletesAndRecreates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab deploy-key list -R group1/project1 -F json --per-page 100": {
			RC: 0, Stdout: `[{"id":1,"title":"CI","key":"ssh-rsa OLD","can_push":false}]`,
		},
		"glab deploy-key delete 1 -R group1/project1":    {RC: 0},
		"glab deploy-key add -R group1/project1 -t CI -": {RC: 0},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "CI", "key": "ssh-rsa NEW",
	}
	res, err := moduleGitlabDeployKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	sawDelete, sawAdd := false, false
	for _, c := range conn.Commands {
		if c == "glab deploy-key delete 1 -R group1/project1" {
			sawDelete = true
		}
		if c == "glab deploy-key add -R group1/project1 -t CI -" {
			sawAdd = true
		}
	}
	if !sawDelete || !sawAdd {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleGitlabDeployKeyAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab deploy-key list -R group1/project1 -F json --per-page 100": {
			RC: 0, Stdout: `[{"id":1,"title":"CI","key":"ssh-rsa AAAA...","can_push":false}]`,
		},
		"glab deploy-key delete 1 -R group1/project1": {RC: 0},
	})
	args := map[string]any{
		"project": "group1/project1", "title": "CI", "key": "ssh-rsa AAAA...", "state": "absent",
	}
	res, err := moduleGitlabDeployKey(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
