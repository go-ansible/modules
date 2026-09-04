package modules

import (
	"context"
	"io"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// queueConn is a scripted connection keyed by exact command string,
// like fakeConn, but supporting a QUEUE of responses per command so a
// test can script a command run more than once (e.g. apt_repo.go's own
// before/after `apt-repo` snapshot, or apt_rpm.go's own repeated
// `rpm -q` probes) returning a different result each time. A queue
// with one entry repeats it for every call; a queue with more than one
// is consumed in order and then repeats its last entry.
type queueConn struct {
	on       map[string][]remoteexec.Result
	Commands []string
}

func (f *queueConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	f.Commands = append(f.Commands, cmd)
	if q, ok := f.on[cmd]; ok && len(q) > 0 {
		r := q[0]
		if len(q) > 1 {
			f.on[cmd] = q[1:]
		}
		return r, nil
	}
	return remoteexec.Result{}, nil
}

func (f *queueConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return nil
}
func (f *queueConn) Fetch(ctx context.Context, remotePath, localPath string) error { return nil }
func (f *queueConn) Remove(ctx context.Context, remotePath string) error           { return nil }
func (f *queueConn) TempPath(base string) string                                   { return "/tmp/" + base }
func (f *queueConn) Close() error                                                  { return nil }

var _ remoteexec.Connection = (*queueConn)(nil)

const aptRepoBare = "env LANGUAGE=C LC_ALL=C /usr/bin/apt-repo"

func TestModuleAptRepoAdd(t *testing.T) {
	conn := &queueConn{on: map[string][]remoteexec.Result{
		"test -e /usr/bin/apt-repo":   {{RC: 0}},
		aptRepoBare:                   {{RC: 0, Stdout: ""}, {RC: 0, Stdout: "Sisyphus\n"}},
		aptRepoBare + " add Sisyphus": {{RC: 0}},
	}}
	res, err := moduleAptRepo(context.Background(), conn, map[string]any{"repo": "Sisyphus"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["repo"] != "Sisyphus" || res.Extra["state"] != "present" {
		t.Fatalf("extra = %+v", res.Extra)
	}
}

func TestModuleAptRepoUnchanged(t *testing.T) {
	conn := &queueConn{on: map[string][]remoteexec.Result{
		"test -e /usr/bin/apt-repo":   {{RC: 0}},
		aptRepoBare:                   {{RC: 0, Stdout: "Sisyphus\n"}},
		aptRepoBare + " add Sisyphus": {{RC: 0}},
	}}
	res, err := moduleAptRepo(context.Background(), conn, map[string]any{"repo": "Sisyphus"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged (listing identical before/after)", res)
	}
}

func TestModuleAptRepoRemoveOthers(t *testing.T) {
	conn := &queueConn{on: map[string][]remoteexec.Result{
		"test -e /usr/bin/apt-repo":   {{RC: 0}},
		aptRepoBare:                   {{RC: 0, Stdout: "old\n"}, {RC: 0, Stdout: "Sisyphus\n"}},
		aptRepoBare + " add Sisyphus": {{RC: 0}},
		aptRepoBare + " rm all":       {{RC: 0}},
	}}
	res, err := moduleAptRepo(context.Background(), conn, map[string]any{
		"repo": "Sisyphus", "remove_others": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	wantOrder := []string{
		"test -e /usr/bin/apt-repo",
		aptRepoBare,
		aptRepoBare + " add Sisyphus",
		aptRepoBare + " rm all",
		aptRepoBare + " add Sisyphus",
		aptRepoBare,
	}
	if len(conn.Commands) != len(wantOrder) {
		t.Fatalf("commands = %v, want %v", conn.Commands, wantOrder)
	}
	for i, w := range wantOrder {
		if conn.Commands[i] != w {
			t.Fatalf("commands[%d] = %q, want %q (full: %v)", i, conn.Commands[i], w, conn.Commands)
		}
	}
}

func TestModuleAptRepoAbsent(t *testing.T) {
	conn := &queueConn{on: map[string][]remoteexec.Result{
		"test -e /usr/bin/apt-repo": {{RC: 0}},
		aptRepoBare:                 {{RC: 0, Stdout: "Sisyphus\n"}, {RC: 0, Stdout: ""}},
		aptRepoBare + " rm all":     {{RC: 0}},
	}}
	res, err := moduleAptRepo(context.Background(), conn, map[string]any{"repo": "all", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptRepoUpdate(t *testing.T) {
	conn := &queueConn{on: map[string][]remoteexec.Result{
		"test -e /usr/bin/apt-repo":   {{RC: 0}},
		aptRepoBare:                   {{RC: 0, Stdout: ""}, {RC: 0, Stdout: "Sisyphus\n"}},
		aptRepoBare + " add Sisyphus": {{RC: 0}},
		aptRepoBare + " update":       {{RC: 0}},
	}}
	res, err := moduleAptRepo(context.Background(), conn, map[string]any{
		"repo": "Sisyphus", "update": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == aptRepoBare+" update" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an apt-repo update call, got %v", conn.Commands)
	}
}

func TestModuleAptRepoMissingTool(t *testing.T) {
	conn := &queueConn{on: map[string][]remoteexec.Result{
		"test -e /usr/bin/apt-repo": {{RC: 1}},
	}}
	res, err := moduleAptRepo(context.Background(), conn, map[string]any{"repo": "Sisyphus"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when apt-repo is missing")
	}
}

func TestModuleAptRepoMissingRepo(t *testing.T) {
	conn := &queueConn{}
	if _, err := moduleAptRepo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing repo")
	}
}

func TestModuleAptRepoBadState(t *testing.T) {
	conn := &queueConn{}
	if _, err := moduleAptRepo(context.Background(), conn, map[string]any{
		"repo": "x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
