package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabGroupVariableCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/variables?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api groups/my_group/variables -X POST --input -":                {RC: 0},
	})
	args := map[string]any{
		"group":     "my_group",
		"variables": []any{map[string]any{"name": "KEY", "value": "val"}},
	}
	res, err := moduleGitlabGroupVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	summary := res.Extra["group_variable"].(map[string]any)
	if added := summary["added"].([]string); len(added) != 1 || added[0] != "KEY" {
		t.Fatalf("added = %#v", summary["added"])
	}
}

func TestModuleGitlabGroupVariableIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/variables?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"key":"KEY","value":"val","environment_scope":"*","variable_type":"env_var"}]`,
		},
	})
	args := map[string]any{
		"group":     "my_group",
		"variables": []any{map[string]any{"name": "KEY", "value": "val"}},
	}
	res, err := moduleGitlabGroupVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupVariableHiddenAlwaysUpdates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/variables?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"key":"KEY","value":"val","environment_scope":"*","variable_type":"env_var"}]`,
		},
		"glab api groups/my_group/variables/KEY -X PUT --input -": {RC: 0},
	})
	args := map[string]any{
		"group":     "my_group",
		"variables": []any{map[string]any{"name": "KEY", "value": "val", "hidden": true}},
	}
	res, err := moduleGitlabGroupVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabGroupVariablePurge(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'groups/my_group/variables?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"key":"KEY","value":"val","environment_scope":"*","variable_type":"env_var"},{"key":"OLD","value":"x","environment_scope":"*","variable_type":"env_var"}]`,
		},
		"glab api groups/my_group/variables/OLD -X DELETE": {RC: 0},
	})
	args := map[string]any{
		"group":     "my_group",
		"variables": []any{map[string]any{"name": "KEY", "value": "val"}},
		"purge":     true,
	}
	res, err := moduleGitlabGroupVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	summary := res.Extra["group_variable"].(map[string]any)
	if removed := summary["removed"].([]string); len(removed) != 1 || removed[0] != "OLD" {
		t.Fatalf("removed = %#v", summary["removed"])
	}
}
