package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabGroupMembersAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/members/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api 'users?username=alice' -X GET":                                {RC: 0, Stdout: `[{"id":7,"username":"alice"}]`},
		"glab api groups/my_group/members -X POST --input -":                    {RC: 0},
	})
	args := map[string]any{
		"gitlab_group": "my_group", "gitlab_user": "alice", "access_level": "developer",
	}
	res, err := moduleGitlabGroupMembers(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupMembersIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/members/all?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"id":7,"username":"alice","access_level":30}]`,
		},
		"glab api 'users?username=alice' -X GET": {RC: 0, Stdout: `[{"id":7,"username":"alice"}]`},
	})
	args := map[string]any{
		"gitlab_group": "my_group", "gitlab_user": "alice", "access_level": "developer",
	}
	res, err := moduleGitlabGroupMembers(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupMembersAbsentRemoves(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/members/all?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"id":7,"username":"alice","access_level":30}]`,
		},
		"glab api 'users?username=alice' -X GET":       {RC: 0, Stdout: `[{"id":7,"username":"alice"}]`},
		"glab api groups/my_group/members/7 -X DELETE": {RC: 0},
	})
	args := map[string]any{"gitlab_group": "my_group", "gitlab_user": "alice", "state": "absent"}
	res, err := moduleGitlabGroupMembers(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupMembersUnknownUserFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/members/all?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api 'users?username=ghost' -X GET":                                {RC: 0, Stdout: "[]"},
	})
	args := map[string]any{"gitlab_group": "my_group", "gitlab_user": "ghost", "access_level": "developer"}
	res, err := moduleGitlabGroupMembers(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an unknown GitLab user")
	}
}
