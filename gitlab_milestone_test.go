package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabMilestoneCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/group1%2Fproject1/milestones?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api projects/group1%2Fproject1/milestones -X POST --input -":                {RC: 0, Stdout: `{"id":1,"title":"m1"}`},
	})
	args := map[string]any{
		"project":    "group1/project1",
		"milestones": []any{map[string]any{"title": "m1"}},
	}
	res, err := moduleGitlabMilestone(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	summary := res.Extra["milestones"].(map[string]any)
	if added := summary["added"].([]string); len(added) != 1 || added[0] != "m1" {
		t.Fatalf("added = %#v", summary["added"])
	}
}

func TestModuleGitlabMilestoneIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/group1%2Fproject1/milestones?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"id":5,"title":"m1"}]`},
	})
	args := map[string]any{
		"project":    "group1/project1",
		"milestones": []any{map[string]any{"title": "m1"}},
	}
	res, err := moduleGitlabMilestone(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("expected only the binary check and list call, got %v", conn.Commands)
	}
}

func TestModuleGitlabMilestoneAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/group1%2Fproject1/milestones?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"id":5,"title":"m1"}]`},
		"glab api projects/group1%2Fproject1/milestones/5 -X DELETE":                      {RC: 0},
	})
	args := map[string]any{
		"project":    "group1/project1",
		"milestones": []any{map[string]any{"title": "m1"}},
		"state":      "absent",
	}
	res, err := moduleGitlabMilestone(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	summary := res.Extra["milestones"].(map[string]any)
	if removed := summary["removed"].([]string); len(removed) != 1 || removed[0] != "m1" {
		t.Fatalf("removed = %#v", summary["removed"])
	}
}

func TestModuleGitlabMilestoneRequiresProjectXorGroup(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleGitlabMilestone(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error when neither project nor group is set")
	}
}
