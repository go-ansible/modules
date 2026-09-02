package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleDpkgSelectionsSet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg --get-selections curl 2>/dev/null || true": {RC: 0, Stdout: "curl\t\t\tinstall\n"},
		"echo 'curl hold' | dpkg --set-selections":       {RC: 0},
	})
	res, err := moduleDpkgSelections(context.Background(), conn, map[string]any{
		"name": "curl", "selection": "hold",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDpkgSelectionsAlreadySet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg --get-selections curl 2>/dev/null || true": {RC: 0, Stdout: "curl\thold\n"},
	})
	res, err := moduleDpkgSelections(context.Background(), conn, map[string]any{
		"name": "curl", "selection": "hold",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no set-selections attempted, commands = %v", conn.Commands)
	}
}

func TestModuleDpkgSelectionsUnknownPackage(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg --get-selections curl 2>/dev/null || true": {RC: 0, Stdout: ""},
		"echo 'curl install' | dpkg --set-selections":    {RC: 0},
	})
	res, err := moduleDpkgSelections(context.Background(), conn, map[string]any{
		"name": "curl", "selection": "install",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleDpkgSelectionsMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDpkgSelections(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleDpkgSelections(context.Background(), conn, map[string]any{"name": "curl"}); err == nil {
		t.Fatal("want error for missing selection")
	}
}

func TestModuleDpkgSelectionsInvalidSelection(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDpkgSelections(context.Background(), conn, map[string]any{
		"name": "curl", "selection": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid selection")
	}
}

func TestDpkgSelectionMatches(t *testing.T) {
	out := "curl\tinstall\ngit\thold\n"
	if !dpkgSelectionMatches(out, "curl", "install") {
		t.Fatal("want match")
	}
	if dpkgSelectionMatches(out, "curl", "hold") {
		t.Fatal("want no match")
	}
	if dpkgSelectionMatches(out, "missing", "install") {
		t.Fatal("want no match for missing package")
	}
}
