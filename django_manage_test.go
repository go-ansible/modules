package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDjangoManageBasic(t *testing.T) {
	want := "cd /srv/app && ./manage.py migrate"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "migrate", "project_path": "/srv/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageChdirAlias(t *testing.T) {
	want := "cd /srv/app && ./manage.py migrate"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "migrate", "chdir": "/srv/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q (via chdir alias)", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageCollectstatic(t *testing.T) {
	want := "cd /srv/app && ./manage.py collectstatic --noinput --clear --link"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "collectstatic", "project_path": "/srv/app", "clear": true, "link": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageMigrateMergeSkipDatabase(t *testing.T) {
	want := "cd /srv/app && ./manage.py migrate --merge --skip --database mydb"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "migrate", "project_path": "/srv/app", "merge": true, "skip": true, "database": "mydb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageTest(t *testing.T) {
	want := "cd /srv/app && ./manage.py test main.SmokeTest other.Test --failfast --testrunner my.Runner"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "test", "project_path": "/srv/app",
		"apps": "main.SmokeTest other.Test", "failfast": true, "testrunner": "my.Runner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageTestFailFastAlias(t *testing.T) {
	want := "cd /srv/app && ./manage.py test --failfast"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "test", "project_path": "/srv/app", "fail_fast": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q (via fail_fast alias)", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageLoaddata(t *testing.T) {
	want := "cd /srv/app && ./manage.py loaddata initial_data more_data"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "loaddata", "project_path": "/srv/app", "fixtures": "initial_data more_data",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageCreatecachetable(t *testing.T) {
	want := "cd /srv/app && ./manage.py createcachetable my_cache_table --database mydb"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "createcachetable", "project_path": "/srv/app",
		"cache_table": "my_cache_table", "database": "mydb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageInlineFlagsInCommand(t *testing.T) {
	want := "cd /srv/app && ./manage.py createsuperuser --noinput --username=admin"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "createsuperuser --noinput --username=admin", "project_path": "/srv/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageSettingsAndPythonpath(t *testing.T) {
	want := "cd /srv/app && ./manage.py migrate --settings proj.settings --pythonpath /x"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "migrate", "project_path": "/srv/app", "settings": "proj.settings", "python_path": "/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q (via python_path alias)", conn.Commands[0], want)
	}
}

func TestModuleDjangoManageVirtualenv(t *testing.T) {
	want := "cd /srv/app && /opt/venv/bin/python manage.py migrate"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/venv": {RC: 0},
		want:                {RC: 0},
	})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "migrate", "project_path": "/srv/app", "virtualenv": "/opt/venv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[len(conn.Commands)-1] != want {
		t.Fatalf("last cmd = %q, want %q", conn.Commands[len(conn.Commands)-1], want)
	}
}

func TestModuleDjangoManageVirtualenvMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /opt/venv": {RC: 1},
	})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "migrate", "project_path": "/srv/app", "virtualenv": "/opt/venv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when virtualenv does not exist")
	}
}

func TestModuleDjangoManageNonZero(t *testing.T) {
	want := "cd /srv/app && ./manage.py migrate"
	conn := newFakeConn(map[string]remoteexec.Result{
		want: {RC: 1, Stderr: "boom"},
	})
	res, err := moduleDjangoManage(context.Background(), conn, map[string]any{
		"command": "migrate", "project_path": "/srv/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-zero exit")
	}
}

func TestModuleDjangoManageMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDjangoManage(context.Background(), conn, map[string]any{"project_path": "/srv/app"}); err == nil {
		t.Fatal("want error for missing command")
	}
	if _, err := moduleDjangoManage(context.Background(), conn, map[string]any{"command": "migrate"}); err == nil {
		t.Fatal("want error for missing project_path")
	}
}
