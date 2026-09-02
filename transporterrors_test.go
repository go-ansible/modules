package modules

import (
	"context"
	"os"
	"testing"
)

// TestTransportErrorsPropagate exercises the "the connection itself
// failed" branch of every module against errConn (see args_test.go),
// which fails every Exec/Put/Fetch/Remove call. Each module should
// return a non-nil Go error (not just a Result{Failed:true}) since a
// transport failure is not the target's own business logic failing.
func TestTransportErrorsPropagate(t *testing.T) {
	conn := errConn{}
	ctx := context.Background()

	cases := []struct {
		name string
		fn   Func
		args map[string]any
	}{
		{"command", moduleCommand, map[string]any{"cmd": "echo hi"}},
		{"shell", moduleShell, map[string]any{"cmd": "echo hi"}},
		{"stat", moduleStat, map[string]any{"path": "/x"}},
		{"file directory", moduleFile, map[string]any{"path": "/x", "state": "directory"}},
		{"file absent", moduleFile, map[string]any{"path": "/x", "state": "absent"}},
		{"file touch", moduleFile, map[string]any{"path": "/x", "state": "touch"}},
		{"file link", moduleFile, map[string]any{"path": "/x", "state": "link", "src": "/y"}},
		{"file file", moduleFile, map[string]any{"path": "/x", "state": "file"}},
		{"copy", moduleCopy, map[string]any{"content": "x", "dest": "/x"}},
		{"lineinfile", moduleLineinfile, map[string]any{"path": "/x", "line": "y"}},
		{"replace", moduleReplace, map[string]any{"path": "/x", "regexp": "a", "replace": "b"}},
		{"template", moduleTemplate, map[string]any{"src": mustTempTemplate(t), "dest": "/x"}},
		{"cron", moduleCron, map[string]any{"name": "n", "job": "j"}},
		{"user", moduleUser, map[string]any{"name": "n"}},
		{"group", moduleGroup, map[string]any{"name": "n"}},
		{"systemd", moduleSystemd, map[string]any{"name": "n", "state": "started"}},
		{"apt", moduleApt, map[string]any{"name": "n"}},
		{"pip", modulePip, map[string]any{"name": "n"}},
		{"git", moduleGit, map[string]any{"repo": "r", "dest": "/x"}},
		{"ping", modulePing, map[string]any{}},
		{"slurp", moduleSlurp, map[string]any{"src": "/x"}},
		{"tempfile", moduleTempfile, map[string]any{}},
		{"fetch", moduleFetch, map[string]any{"src": "/x", "dest": "/y"}},
		{"find", moduleFind, map[string]any{"paths": "/x"}},
		{"get_url", moduleGetURL, map[string]any{"url": "http://x", "dest": "/y"}},
		{"uri", moduleURI, map[string]any{"url": "http://x"}},
		{"wait_for", moduleWaitFor, map[string]any{"port": 22}},
		{"hostname", moduleHostname, map[string]any{"name": "x"}},
		{"getent", moduleGetent, map[string]any{"database": "passwd"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.fn(ctx, conn, c.args)
			if err == nil {
				t.Fatalf("%s: want a transport error, got nil", c.name)
			}
		})
	}
}

func mustTempTemplate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := dir + "/t.j2"
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
