package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGunicornStarts(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rm -f /tmp/gunicorn.temp.pid /tmp/gunicorn.temp.error.log":                                  {RC: 0},
		"gunicorn -D --error-logfile /tmp/gunicorn.temp.error.log --pid /tmp/gunicorn.temp.pid wsgi": {RC: 0, Stderr: ""},
		"test -e /tmp/gunicorn.temp.pid":                                                             {RC: 0},
		"head -n1 /tmp/gunicorn.temp.pid":                                                            {RC: 0, Stdout: "1234\n"},
		"rm -f /tmp/gunicorn.temp.pid":                                                               {RC: 0},
	})
	res, err := moduleGunicorn(context.Background(), conn, map[string]any{"app": "wsgi"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["gunicorn"] != "1234" {
		t.Fatalf("gunicorn pid = %v", res.Extra["gunicorn"])
	}
}

func TestModuleGunicornStderrFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rm -f /tmp/gunicorn.temp.pid /tmp/gunicorn.temp.error.log":                                  {RC: 0},
		"gunicorn -D --error-logfile /tmp/gunicorn.temp.error.log --pid /tmp/gunicorn.temp.pid wsgi": {RC: 1, Stderr: "boom"},
	})
	res, err := moduleGunicorn(context.Background(), conn, map[string]any{"app": "wsgi"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when stderr is non-empty")
	}
}

func TestModuleGunicornVenvAndPid(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"rm -f /tmp/gunicorn.temp.pid /tmp/gunicorn.temp.error.log":                                                                       {RC: 0},
		"/workspace/example/venv/bin/gunicorn -D --error-logfile /tmp/gunicorn.temp.error.log --pid /workspace/example/gunicorn.pid wsgi": {RC: 0},
		"test -e /workspace/example/gunicorn.pid":                                                                                         {RC: 0},
		"head -n1 /workspace/example/gunicorn.pid":                                                                                        {RC: 0, Stdout: "5678\n"},
	})
	res, err := moduleGunicorn(context.Background(), conn, map[string]any{
		"app": "wsgi", "venv": "/workspace/example/venv", "pid": "/workspace/example/gunicorn.pid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["gunicorn"] != "5678" {
		t.Fatalf("gunicorn pid = %v", res.Extra["gunicorn"])
	}
	// pid was explicitly given, so it should NOT be removed afterward.
	for _, c := range conn.Commands {
		if c == "rm -f /workspace/example/gunicorn.pid" {
			t.Fatal("want given pid file left in place")
		}
	}
}

func TestModuleGunicornMissingApp(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleGunicorn(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing app")
	}
}
