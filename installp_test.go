package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleInstallpInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"installp -l -MR -d /repository/AIX71/installp/base": {RC: 0, Stdout: "foo 1.0.0.0  C     F    foo fileset\n"},
		"lslpp -lcq 'foo*'": {RC: 1, Stderr: "lslpp: 0505-072  foo: not installed."},
		"installp -a -Y -X -d /repository/AIX71/installp/base foo": {RC: 0},
	})
	res, err := moduleInstallp(context.Background(), conn, map[string]any{
		"name": "foo", "repository_path": "/repository/AIX71/installp/base", "accept_license": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleInstallpAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"installp -l -MR -d /repo": {RC: 0, Stdout: "foo 1.0.0.0  C     F    foo fileset\n"},
		"lslpp -lcq 'foo*'":        {RC: 0},
	})
	res, err := moduleInstallp(context.Background(), conn, map[string]any{
		"name": "foo", "repository_path": "/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleInstallpNotFoundInRepository(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"installp -l -MR -d /repo": {RC: 0, Stdout: "bar 1.0.0.0  C     F    bar fileset\n"},
	})
	res, err := moduleInstallp(context.Background(), conn, map[string]any{
		"name": "foo", "repository_path": "/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: package not found in repository is not an install")
	}
}

func TestModuleInstallpAllPackages(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lslpp -lcq 'all*'":           {RC: 1, Stderr: "lslpp: 0505-072  all: not installed."},
		"installp -a -X -d /repo all": {RC: 0},
	})
	res, err := moduleInstallp(context.Background(), conn, map[string]any{
		"name": "all", "repository_path": "/repo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v, want no repository listing for name=all", conn.Commands)
	}
}

func TestModuleInstallpMissingRepositoryPath(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleInstallp(context.Background(), conn, map[string]any{"name": "foo"}); err == nil {
		t.Fatal("want error: repository_path required for state=present")
	}
}

func TestModuleInstallpRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lslpp -lcq 'foo*'": {RC: 0},
		"installp -u foo":   {RC: 0},
	})
	res, err := moduleInstallp(context.Background(), conn, map[string]any{"name": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleInstallpRemoveNotInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lslpp -lcq 'foo*'": {RC: 1, Stderr: "lslpp: 0505-072  foo: not installed."},
	})
	res, err := moduleInstallp(context.Background(), conn, map[string]any{"name": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleInstallpMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleInstallp(context.Background(), conn, map[string]any{"state": "absent"}); err == nil {
		t.Fatal("want error for missing name")
	}
}
