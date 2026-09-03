package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLbuCommitDirty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lbu status": {RC: 0, Stdout: "etc/hostname\n"},
		"lbu commit": {RC: 0},
	})
	res, err := moduleLbu(context.Background(), conn, map[string]any{"commit": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLbuCommitClean(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lbu status": {RC: 0, Stdout: ""},
	})
	res, err := moduleLbu(context.Background(), conn, map[string]any{"commit": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when nothing pending")
	}
}

func TestModuleLbuInclude(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lbu include -l": {RC: 0, Stdout: "root/.ssh/authorized_keys\n"},
		"lbu include /root/.ssh/authorized_keys /var/lib/misc": {RC: 0},
	})
	res, err := moduleLbu(context.Background(), conn, map[string]any{
		"include": []any{"/root/.ssh/authorized_keys", "/var/lib/misc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: /var/lib/misc is new")
	}
}

func TestModuleLbuIncludeAlreadyTracked(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lbu include -l": {RC: 0, Stdout: "root/.ssh/authorized_keys\n"},
	})
	res, err := moduleLbu(context.Background(), conn, map[string]any{
		"include": []any{"/root/.ssh/authorized_keys"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already tracked")
	}
}

func TestModuleLbuExcludeAndCommit(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lbu exclude -l":       {RC: 0, Stdout: ""},
		"lbu exclude /etc/opt": {RC: 0},
		"lbu commit":           {RC: 0},
	})
	res, err := moduleLbu(context.Background(), conn, map[string]any{
		"commit": true, "exclude": []any{"/etc/opt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
