package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabLabelCreate(t *testing.T) {
	listCmd := "glab api 'projects/group1%2Fproject1/labels?per_page=100' -X GET --paginate"
	createCmd := "glab api projects/group1%2Fproject1/labels -X POST --input -"
	conn := newSeqConn(map[string][]remoteexec.Result{
		listCmd: {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":1,"name":"bug","color":"#FF0000"}]`},
		},
		createCmd: {{RC: 0, Stdout: `{"id":1,"name":"bug","color":"#FF0000"}`}},
	})
	args := map[string]any{
		"project": "group1/project1",
		"labels":  []any{map[string]any{"name": "bug", "color": "#FF0000"}},
	}
	res, err := moduleGitlabLabel(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	summary := res.Extra["labels"].(map[string]any)
	if added := summary["added"].([]string); len(added) != 1 || added[0] != "bug" {
		t.Fatalf("added = %#v", summary["added"])
	}
}

func TestModuleGitlabLabelIdempotent(t *testing.T) {
	listCmd := "glab api 'projects/group1%2Fproject1/labels?per_page=100' -X GET --paginate"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd: {RC: 0, Stdout: `[{"id":1,"name":"bug","color":"#FF0000"}]`},
	})
	args := map[string]any{
		"project": "group1/project1",
		"labels":  []any{map[string]any{"name": "bug", "color": "#FF0000"}},
	}
	res, err := moduleGitlabLabel(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabLabelGroupScopeUsesAPIUniformly(t *testing.T) {
	listCmd := "glab api 'groups/my_group/labels?per_page=100' -X GET --paginate"
	createCmd := "glab api groups/my_group/labels -X POST --input -"
	conn := newSeqConn(map[string][]remoteexec.Result{
		listCmd: {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":1,"name":"bug","color":"#FF0000"}]`},
		},
		createCmd: {{RC: 0, Stdout: `{"id":1,"name":"bug","color":"#FF0000"}`}},
	})
	args := map[string]any{
		"group":  "my_group",
		"labels": []any{map[string]any{"name": "bug", "color": "#FF0000"}},
	}
	res, err := moduleGitlabLabel(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabLabelAbsentDeletes(t *testing.T) {
	listCmd := "glab api 'projects/group1%2Fproject1/labels?per_page=100' -X GET --paginate"
	deleteCmd := "glab api projects/group1%2Fproject1/labels/bug -X DELETE"
	conn := newFakeConn(map[string]remoteexec.Result{
		listCmd:   {RC: 0, Stdout: `[{"id":1,"name":"bug","color":"#FF0000"}]`},
		deleteCmd: {RC: 0},
	})
	args := map[string]any{
		"project": "group1/project1",
		"labels":  []any{map[string]any{"name": "bug"}},
		"state":   "absent",
	}
	res, err := moduleGitlabLabel(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabLabelRequiresProjectXorGroup(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGitlabLabel(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when neither project nor group is set")
	}
}
