package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGolangPackageInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"go env GOBIN":                       {RC: 0, Stdout: ""},
		"go env GOPATH":                      {RC: 0, Stdout: "/home/user/go"},
		"test -e /home/user/go/bin/stringer": {RC: 1},
		"go install golang.org/x/tools/cmd/stringer@latest": {RC: 0},
	})
	res, err := moduleGolangPackage(context.Background(), conn, map[string]any{
		"name": "golang.org/x/tools/cmd/stringer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 4 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleGolangPackageAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"go env GOBIN":                   {RC: 0, Stdout: "/custom/gobin"},
		"test -e /custom/gobin/stringer": {RC: 0},
	})
	res, err := moduleGolangPackage(context.Background(), conn, map[string]any{
		"name": "golang.org/x/tools/cmd/stringer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleGolangPackageAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"go env GOBIN":                   {RC: 0, Stdout: "/custom/gobin"},
		"test -e /custom/gobin/stringer": {RC: 0},
		"rm -f /custom/gobin/stringer":   {RC: 0},
	})
	res, err := moduleGolangPackage(context.Background(), conn, map[string]any{
		"name":  "golang.org/x/tools/cmd/stringer",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleGolangPackageLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"go env GOBIN": {RC: 0, Stdout: "/custom/gobin"},
		"go install golang.org/x/tools/cmd/stringer@latest": {RC: 0},
	})
	res, err := moduleGolangPackage(context.Background(), conn, map[string]any{
		"name":  "golang.org/x/tools/cmd/stringer",
		"state": "latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v, want no presence query for state=latest", conn.Commands)
	}
}

func TestModuleGolangPackageVersionWithMultipleNamesRejected(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleGolangPackage(context.Background(), conn, map[string]any{
		"name":    []any{"golang.org/x/tools/cmd/stringer", "golang.org/x/tools/cmd/goimports"},
		"version": "v1.0.0",
	})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestModuleGolangPackageMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGolangPackage(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
