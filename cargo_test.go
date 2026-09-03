package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleCargoInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cargo install --list":   {RC: 0, Stdout: ""},
		"cargo install ludusavi": {RC: 0},
	})
	res, err := moduleCargo(context.Background(), conn, map[string]any{"name": "ludusavi"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleCargoAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cargo install --list": {RC: 0, Stdout: "ludusavi v0.10.0:\n    ludusavi\n"},
	})
	res, err := moduleCargo(context.Background(), conn, map[string]any{"name": "ludusavi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleCargoAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cargo install --list":     {RC: 0, Stdout: "ludusavi v0.10.0:\n    ludusavi\n"},
		"cargo uninstall ludusavi": {RC: 0},
	})
	res, err := moduleCargo(context.Background(), conn, map[string]any{
		"name": "ludusavi", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleCargoLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cargo install --force ludusavi": {RC: 0},
	})
	res, err := moduleCargo(context.Background(), conn, map[string]any{
		"name": "ludusavi", "state": "latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no query for state=latest", conn.Commands)
	}
}

func TestModuleCargoDirectory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cargo install --path /path/to/ludusavi/source": {RC: 0},
	})
	res, err := moduleCargo(context.Background(), conn, map[string]any{
		"directory": "/path/to/ludusavi/source",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no query in directory mode", conn.Commands)
	}
}

func TestModuleCargoDirectoryAbsentRejected(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleCargo(context.Background(), conn, map[string]any{
		"directory": "/path/to/source", "state": "absent",
	})
	if err == nil {
		t.Fatal("want error: directory + absent is unsupported")
	}
}

func TestModuleCargoMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleCargo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
