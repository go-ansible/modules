package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMksysbDefaults(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -d /repository/images":                        {RC: 0},
		"mksysb -X -p -a -A -i /repository/images/myserver": {RC: 0, Stdout: "Creating information file...\n"},
	})
	res, err := moduleMksysb(context.Background(), conn, map[string]any{
		"name": "myserver", "storage_path": "/repository/images",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMksysbExcludeFlags(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -d /repository/images":                              {RC: 0},
		"mksysb -X -e -G -p -a -A -i /repository/images/myserver": {RC: 0},
	})
	res, err := moduleMksysb(context.Background(), conn, map[string]any{
		"name": "myserver", "storage_path": "/repository/images",
		"exclude_files": true, "exclude_wpar_files": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleMksysbBadStoragePath(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -d /nope": {RC: 1},
	})
	res, err := moduleMksysb(context.Background(), conn, map[string]any{
		"name": "myserver", "storage_path": "/nope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: storage_path is not a directory")
	}
}

func TestModuleMksysbMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMksysb(context.Background(), conn, map[string]any{"name": "myserver"}); err == nil {
		t.Fatal("want error for missing storage_path")
	}
}
