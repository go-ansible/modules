package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleComposerInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"composer --working-dir=/srv/app install --no-dev --optimize-autoloader --no-ansi --no-interaction --no-progress": {RC: 0},
	})
	res, err := moduleComposer(context.Background(), conn, map[string]any{
		"working_dir": "/srv/app",
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

func TestModuleComposerRequireGlobal(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"composer global require --no-dev --optimize-autoloader --no-ansi --no-interaction --no-progress my/package": {RC: 0},
	})
	res, err := moduleComposer(context.Background(), conn, map[string]any{
		"command":        "require",
		"arguments":      "my/package",
		"global_command": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleComposerCreateProjectSkipsIfExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/app/composer.json": {RC: 0},
	})
	res, err := moduleComposer(context.Background(), conn, map[string]any{
		"command":     "create-project",
		"working_dir": "/srv/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want only the existence probe", conn.Commands)
	}
	if !strings.Contains(res.Msg, "composer.json already exists") {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestModuleComposerCreateProjectForced(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /srv/app/composer.json": {RC: 0},
		"composer --working-dir=/srv/app create-project --no-dev --optimize-autoloader --no-ansi --no-interaction --no-progress package/package /path/to/project": {RC: 0},
	})
	res, err := moduleComposer(context.Background(), conn, map[string]any{
		"command":     "create-project",
		"arguments":   "package/package /path/to/project",
		"working_dir": "/srv/app",
		"force":       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	// force=true skips the composer.json probe entirely.
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleComposerMissingWorkingDir(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleComposer(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing working_dir")
	}
}
