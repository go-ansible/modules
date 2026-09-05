package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRollbarDeploymentSuccess(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rollbar-cli": {RC: 0},
		"rollbar-cli notify-deploy --access-token AAAAAA --environment staging --code-version 4.2 --local-username ansible --rollbar-username admin --comment 'Test Deploy'": {RC: 0},
	})
	res, err := moduleRollbarDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "staging", "revision": "4.2",
		"user": "ansible", "rollbar_user": "admin", "comment": "Test Deploy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRollbarDeploymentMinimal(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rollbar-cli": {RC: 0},
		"rollbar-cli notify-deploy --access-token AAAAAA --environment production --code-version abc123": {RC: 0},
	})
	res, err := moduleRollbarDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "production", "revision": "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRollbarDeploymentFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rollbar-cli": {RC: 0},
		"rollbar-cli notify-deploy --access-token AAAAAA --environment production --code-version abc123": {RC: 1, Stderr: "401 Unauthorized"},
	})
	res, err := moduleRollbarDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "production", "revision": "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleRollbarDeploymentMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rollbar-cli": {RC: 1},
	})
	res, err := moduleRollbarDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "production", "revision": "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleRollbarDeploymentMissingRequired(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRollbarDeployment(context.Background(), conn, map[string]any{
		"environment": "production", "revision": "abc123",
	}); err == nil {
		t.Fatal("want error for missing token")
	}
	if _, err := moduleRollbarDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "revision": "abc123",
	}); err == nil {
		t.Fatal("want error for missing environment")
	}
	if _, err := moduleRollbarDeployment(context.Background(), conn, map[string]any{
		"token": "AAAAAA", "environment": "production",
	}); err == nil {
		t.Fatal("want error for missing revision")
	}
}
