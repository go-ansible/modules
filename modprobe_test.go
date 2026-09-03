package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModprobeLsmodName(t *testing.T) {
	if got := modprobeLsmodName("8021q"); got != "8021q" {
		t.Fatalf("got %q", got)
	}
	if got := modprobeLsmodName("some-module"); got != "some_module" {
		t.Fatalf("got %q", got)
	}
}

func TestModuleModprobePresentLoadsWithParams(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsmod | grep -qw dummy": {RC: 1},
	})
	res, err := moduleModprobe(context.Background(), conn, map[string]any{
		"name": "dummy", "params": "numdummies=2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "modprobe dummy numdummies=2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleModprobePresentAlreadyLoaded(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsmod | grep -qw 8021q": {RC: 0},
	})
	res, err := moduleModprobe(context.Background(), conn, map[string]any{"name": "8021q"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleModprobeAbsentUnloads(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsmod | grep -qw dummy": {RC: 0},
	})
	res, err := moduleModprobe(context.Background(), conn, map[string]any{
		"name": "dummy", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "modprobe -r dummy" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleModprobeAbsentAlreadyUnloaded(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsmod | grep -qw dummy": {RC: 1},
	})
	res, err := moduleModprobe(context.Background(), conn, map[string]any{
		"name": "dummy", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleModprobePersistentPresentWritesFiles(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsmod | grep -qw dummy":                 {RC: 0},
		"test -e /etc/modules-load.d/dummy.conf": {RC: 1},
		"test -e /etc/modprobe.d/dummy.conf":     {RC: 1},
	})
	res, err := moduleModprobe(context.Background(), conn, map[string]any{
		"name": "dummy", "params": "numdummies=2", "persistent": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: persistence files created")
	}
}

func TestModuleModprobeDisabledPersistentIsNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsmod | grep -qw dummy": {RC: 0},
	})
	res, err := moduleModprobe(context.Background(), conn, map[string]any{
		"name": "dummy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: persistent defaults to disabled")
	}
	for _, c := range conn.Commands {
		if c != "lsmod | grep -qw dummy" {
			t.Fatalf("want no persistence probing, commands = %v", conn.Commands)
		}
	}
}

func TestModuleModprobeMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleModprobe(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleModprobeInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleModprobe(context.Background(), conn, map[string]any{
		"name": "dummy", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleModprobeInvalidPersistent(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleModprobe(context.Background(), conn, map[string]any{
		"name": "dummy", "persistent": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid persistent")
	}
}
