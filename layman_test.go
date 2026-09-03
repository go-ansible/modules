package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLaymanInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"layman -l 2>/dev/null": {RC: 0, Stdout: "  cvut  [Git] (source: ...)\n"},
		"layman -a mozilla":     {RC: 0},
	})
	res, err := moduleLayman(context.Background(), conn, map[string]any{"name": "mozilla"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLaymanAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"layman -l 2>/dev/null": {RC: 0, Stdout: "  cvut  [Git] (source: ...)\n"},
	})
	res, err := moduleLayman(context.Background(), conn, map[string]any{"name": "cvut"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleLaymanListURL(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"layman -l 2>/dev/null": {RC: 0, Stdout: ""},
		"layman -o http://raw.github.com/cvut/gentoo-overlay/master/overlay.xml -a cvut": {RC: 0},
	})
	res, err := moduleLayman(context.Background(), conn, map[string]any{
		"name": "cvut", "list_url": "http://raw.github.com/cvut/gentoo-overlay/master/overlay.xml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleLaymanUpdatedAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"layman -s ALL": {RC: 0},
	})
	res, err := moduleLayman(context.Background(), conn, map[string]any{"name": "ALL", "state": "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no -l listing for ALL", conn.Commands)
	}
}

func TestModuleLaymanUninstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"layman -l 2>/dev/null": {RC: 0, Stdout: "  cvut  [Git] (source: ...)\n"},
		"layman -d cvut":        {RC: 0},
	})
	res, err := moduleLayman(context.Background(), conn, map[string]any{"name": "cvut", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleLaymanAllRequiresUpdated(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleLayman(context.Background(), conn, map[string]any{"name": "ALL", "state": "present"})
	if err == nil {
		t.Fatal("want error: name=ALL only valid with state=updated")
	}
}
