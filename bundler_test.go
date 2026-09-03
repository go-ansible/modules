package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleBundlerInstallDefault(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bundle install": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: bundler always reports changed on success")
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != "bundle install" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleBundlerLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bundle update": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{"state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBundlerInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleBundler(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleBundlerGemfile(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bundle install --gemfile /app/Gemfile": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{"gemfile": "/app/Gemfile"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBundlerDeploymentMode(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bundle install --deployment": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{"deployment_mode": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBundlerDeploymentModeIgnoredOnUpdate(t *testing.T) {
	// deployment_mode only applies to the install verb, not update.
	conn := newFakeConn(map[string]remoteexec.Result{
		"bundle update": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{"state": "latest", "deployment_mode": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != "bundle update" {
		t.Fatalf("commands = %v, want deployment_mode ignored for update", conn.Commands)
	}
}

func TestModuleBundlerExcludeGroups(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bundle install --without dev:test": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{
		"exclude_groups": []any{"dev", "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBundlerExtraArgs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bundle install --jobs=4": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{"extra_args": "--jobs=4"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBundlerChdir(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cd /app && bundle install": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{"chdir": "/app"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleBundlerAllOptionsCombined(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cd /app && bundle install --gemfile /app/Gemfile --deployment --without dev:test --jobs=4": {RC: 0},
	})
	res, err := moduleBundler(context.Background(), conn, map[string]any{
		"chdir":           "/app",
		"gemfile":         "/app/Gemfile",
		"deployment_mode": true,
		"exclude_groups":  []any{"dev", "test"},
		"extra_args":      "--jobs=4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleBundlerRunFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"bundle install": {RC: 1, Stderr: "could not resolve dependencies"},
	})
	if _, err := moduleBundler(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when bundle install fails")
	}
}
