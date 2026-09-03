package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUvPythonMissingVersion(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUvPython(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing version")
	}
}

func TestModuleUvPythonAdvancedSelectorRejected(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleUvPython(context.Background(), conn, map[string]any{"version": ">=3.12,<3.13"}); err == nil {
		t.Fatal("want error for advanced selector")
	}
}

const uvListOutput = `cpython-3.14.0-macos-aarch64-none    <download available>
cpython-3.13.5-macos-aarch64-none    /root/.local/share/uv/python/cpython-3.13.5-macos-aarch64-none/bin/python3.13
cpython-3.13.4-macos-aarch64-none    <download available>
cpython-3.12.3-macos-aarch64-none    /root/.local/share/uv/python/cpython-3.12.3-macos-aarch64-none/bin/python3.12
`

func TestModuleUvPythonInstallExactVersionNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uv python list --all-versions 2>/dev/null": {RC: 0, Stdout: uvListOutput},
		"uv python install 3.14.0":                  {RC: 0},
	})
	res, err := moduleUvPython(context.Background(), conn, map[string]any{"version": "3.14.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleUvPythonPresentMinorAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uv python list --all-versions 2>/dev/null": {RC: 0, Stdout: uvListOutput},
	})
	res, err := moduleUvPython(context.Background(), conn, map[string]any{"version": "3.13"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged (3.13.5 already installed), res = %+v", res)
	}
}

const uvListOutputOlderPatchInstalled = `cpython-3.13.5-macos-aarch64-none    <download available>
cpython-3.13.4-macos-aarch64-none    /root/.local/share/uv/python/cpython-3.13.4-macos-aarch64-none/bin/python3.13
`

func TestModuleUvPythonLatestInstallsNewerPatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uv python list --all-versions 2>/dev/null": {RC: 0, Stdout: uvListOutputOlderPatchInstalled},
		"uv python install 3.13.5":                  {RC: 0},
	})
	res, err := moduleUvPython(context.Background(), conn, map[string]any{"version": "3.13", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed: 3.13.4 installed but 3.13.5 is newer, res = %+v", res)
	}
}

func TestModuleUvPythonRemoveExact(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uv python list --all-versions 2>/dev/null": {RC: 0, Stdout: uvListOutput},
		"uv python uninstall 3.13.5":                {RC: 0},
	})
	res, err := moduleUvPython(context.Background(), conn, map[string]any{"version": "3.13.5", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleUvPythonRemoveAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uv python list --all-versions 2>/dev/null": {RC: 0, Stdout: uvListOutput},
	})
	res, err := moduleUvPython(context.Background(), conn, map[string]any{"version": "3.11.0", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleUvPythonInstallFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uv python list --all-versions 2>/dev/null": {RC: 0, Stdout: uvListOutput},
		"uv python install 3.14.0":                  {RC: 1, Stderr: "network error"},
	})
	res, err := moduleUvPython(context.Background(), conn, map[string]any{"version": "3.14.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}
