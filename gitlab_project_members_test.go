package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabProjectMembersAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/members/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api 'users?username=alice' -X GET":                               {RC: 0, Stdout: `[{"id":7,"username":"alice"}]`},
		"glab api projects/g%2Fp/members -X POST --input -":                    {RC: 0},
	})
	args := map[string]any{"project": "g/p", "gitlab_user": "alice", "access_level": "developer"}
	res, err := moduleGitlabProjectMembers(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGitlabProjectMembersIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/members/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"id":7,"username":"alice","access_level":30}]`},
		"glab api 'users?username=alice' -X GET":                               {RC: 0, Stdout: `[{"id":7,"username":"alice"}]`},
	})
	args := map[string]any{"project": "g/p", "gitlab_user": "alice", "access_level": "developer"}
	res, err := moduleGitlabProjectMembers(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleGitlabProjectMembersRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/members/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"id":7,"username":"alice","access_level":30}]`},
		"glab api 'users?username=alice' -X GET":                               {RC: 0, Stdout: `[{"id":7,"username":"alice"}]`},
		"glab api projects/g%2Fp/members/7 -X DELETE":                          {RC: 0},
	})
	args := map[string]any{"project": "g/p", "gitlab_user": "alice", "state": "absent"}
	res, err := moduleGitlabProjectMembers(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGitlabProjectMembersUnknownUserFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/members/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api 'users?username=ghost' -X GET":                               {RC: 0, Stdout: "[]"},
	})
	args := map[string]any{"project": "g/p", "gitlab_user": "ghost", "access_level": "developer"}
	res, err := moduleGitlabProjectMembers(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed for unknown user")
	}
}
