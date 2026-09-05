package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHoneybadgerDeploymentSuccess(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hb": {RC: 0},
		"HONEYBADGER_API_KEY=AAAAAA hb deploy -e staging -r git@github.com:user/repo.git -v b6826b8 -u ansible": {RC: 0, Stdout: "Deployment successfully reported to Honeybadger\n"},
	})
	res, err := moduleHoneybadgerDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "staging", "user": "ansible",
		"revision": "b6826b8", "repo": "git@github.com:user/repo.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	// The API key must never appear on the recorded command's own argv
	// tokens outside of the leading env-var assignment.
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %+v", conn.Commands)
	}
}

func TestModuleHoneybadgerDeploymentMinimal(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hb": {RC: 0},
		"HONEYBADGER_API_KEY=AAAAAA hb deploy -e production": {RC: 0},
	})
	res, err := moduleHoneybadgerDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHoneybadgerDeploymentCustomURL(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hb": {RC: 0},
		"HONEYBADGER_API_KEY=AAAAAA hb deploy -e production --endpoint https://honeybadger.example.com": {RC: 0},
	})
	res, err := moduleHoneybadgerDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "production",
		"url": "https://honeybadger.example.com/v1/deploys",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHoneybadgerDeploymentFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hb": {RC: 0},
		"HONEYBADGER_API_KEY=AAAAAA hb deploy -e production": {RC: 1, Stderr: "unexpected status code: 401"},
	})
	res, err := moduleHoneybadgerDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleHoneybadgerDeploymentMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hb": {RC: 1},
	})
	res, err := moduleHoneybadgerDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "production",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleHoneybadgerDeploymentMissingRequired(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHoneybadgerDeployment(context.Background(), conn, map[string]any{
		"environment": "production",
	}); err == nil {
		t.Fatal("want error for missing token")
	}
	if _, err := moduleHoneybadgerDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA",
	}); err == nil {
		t.Fatal("want error for missing environment")
	}
}
