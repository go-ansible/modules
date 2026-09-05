package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHerokuCollaboratorAddsWhenAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"heroku access -a myapp --json":               {RC: 0, Stdout: "[]"},
		"heroku access:add user@example.com -a myapp": {RC: 0},
	})
	res, err := moduleHerokuCollaborator(context.Background(), conn, map[string]any{
		"user": "user@example.com", "apps": []any{"myapp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleHerokuCollaboratorAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"heroku access -a myapp --json": {RC: 0, Stdout: `[{"user":{"email":"user@example.com"}}]`},
	})
	res, err := moduleHerokuCollaborator(context.Background(), conn, map[string]any{
		"user": "user@example.com", "apps": []any{"myapp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleHerokuCollaboratorRemovesWhenPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"heroku access -a myapp --json":                  {RC: 0, Stdout: `[{"user":{"email":"user@example.com"}}]`},
		"heroku access:remove user@example.com -a myapp": {RC: 0},
	})
	res, err := moduleHerokuCollaborator(context.Background(), conn, map[string]any{
		"user": "user@example.com", "apps": []any{"myapp"}, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleHerokuCollaboratorAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"heroku access -a myapp --json": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleHerokuCollaborator(context.Background(), conn, map[string]any{
		"user": "user@example.com", "apps": []any{"myapp"}, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleHerokuCollaboratorAppMissingFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"heroku access -a myapp --json": {RC: 1, Stderr: "Couldn't find that app."},
	})
	res, err := moduleHerokuCollaborator(context.Background(), conn, map[string]any{
		"user": "user@example.com", "apps": []any{"myapp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleHerokuCollaboratorAPIKeyEnv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"HEROKU_API_KEY=secret heroku access -a myapp --json":               {RC: 0, Stdout: "[]"},
		"HEROKU_API_KEY=secret heroku access:add user@example.com -a myapp": {RC: 0},
	})
	res, err := moduleHerokuCollaborator(context.Background(), conn, map[string]any{
		"user": "user@example.com", "apps": []any{"myapp"}, "api_key": "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleHerokuCollaboratorMissingUser(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleHerokuCollaborator(context.Background(), conn, map[string]any{"apps": []any{"myapp"}})
	if err == nil {
		t.Fatal("want error for missing user")
	}
}
