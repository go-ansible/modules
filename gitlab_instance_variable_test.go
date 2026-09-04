package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGitlabInstanceVariableCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'admin/ci/variables?per_page=100' -X GET --paginate": {RC: 0, Stdout: "[]"},
		"glab api admin/ci/variables -X POST --input -":                {RC: 0},
	})
	args := map[string]any{
		"variables": []any{map[string]any{"name": "KEY", "value": "val"}},
	}
	res, err := moduleGitlabInstanceVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	summary := res.Extra["instance_variable"].(map[string]any)
	if added := summary["added"].([]string); len(added) != 1 || added[0] != "KEY" {
		t.Fatalf("added = %#v", summary["added"])
	}
}

func TestModuleGitlabInstanceVariableIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'admin/ci/variables?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"key":"KEY","value":"val","variable_type":"env_var"}]`,
		},
	})
	args := map[string]any{
		"variables": []any{map[string]any{"name": "KEY", "value": "val"}},
	}
	res, err := moduleGitlabInstanceVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGitlabInstanceVariableAbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"glab api 'admin/ci/variables?per_page=100' -X GET --paginate": {
			RC: 0, Stdout: `[{"key":"KEY","value":"val","variable_type":"env_var"}]`,
		},
		"glab api admin/ci/variables/KEY -X DELETE": {RC: 0},
	})
	args := map[string]any{
		"variables": []any{map[string]any{"name": "KEY"}},
		"state":     "absent",
	}
	res, err := moduleGitlabInstanceVariable(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
