package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabProjectVariableCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/variables?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api projects/g%2Fp/variables -X POST --input -":                {RC: 0},
	})
	args := map[string]any{"project": "g/p", "vars": map[string]any{"FOO": "bar"}}
	res, err := moduleGitlabProjectVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	summary := res.Extra["project_variable"].(map[string]any)
	if added := summary["added"].([]string); len(added) != 1 || added[0] != "FOO" {
		t.Fatalf("added = %#v", summary["added"])
	}
}

func TestModuleGitlabProjectVariableIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/variables?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"key":"FOO","value":"bar","environment_scope":"*","variable_type":"env_var"}]`},
	})
	args := map[string]any{"project": "g/p", "vars": map[string]any{"FOO": "bar"}}
	res, err := moduleGitlabProjectVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	summary := res.Extra["project_variable"].(map[string]any)
	if untouched := summary["untouched"].([]string); len(untouched) != 1 || untouched[0] != "FOO" {
		t.Fatalf("untouched = %#v", summary["untouched"])
	}
}

func TestModuleGitlabProjectVariableHiddenAlwaysUpdates(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/variables?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"key":"FOO","value":"bar","environment_scope":"*","variable_type":"env_var"}]`},
		"glab api projects/g%2Fp/variables/FOO -X PUT --input -":             {RC: 0},
	})
	args := map[string]any{
		"project": "g/p",
		"variables": []any{
			map[string]any{"name": "FOO", "value": "bar", "hidden": true},
		},
	}
	res, err := moduleGitlabProjectVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed (hidden variables are never idempotent)")
	}
}

func TestModuleGitlabProjectVariableAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'projects/g%2Fp/variables?per_page=100' -X GET --paginate": {RC: 0, Stdout: `[{"key":"FOO","value":"bar","environment_scope":"*"}]`},
		"glab api projects/g%2Fp/variables/FOO -X DELETE":                    {RC: 0},
	})
	args := map[string]any{"project": "g/p", "vars": map[string]any{"FOO": "bar"}, "state": "absent"}
	res, err := moduleGitlabProjectVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
