package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePipxInfoAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxToxList},
	})
	res, err := modulePipxInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, want a plain Ok", res)
	}
	apps := res.Extra["application"].([]any)
	if len(apps) != 1 {
		t.Fatalf("application = %v", apps)
	}
	entry := apps[0].(map[string]any)
	if entry["name"] != "tox" || entry["version"] != "3.24.0" || entry["pinned"] != false {
		t.Fatalf("entry = %v", entry)
	}
}

func TestModulePipxInfoFilterByName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxToxList},
	})
	res, err := modulePipxInfo(context.Background(), conn, map[string]any{"name": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	apps := res.Extra["application"].([]any)
	if len(apps) != 0 {
		t.Fatalf("application = %v, want empty for a non-matching filter", apps)
	}
}

func TestModulePipxInfoIncludeDeps(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxToxList},
	})
	res, err := modulePipxInfo(context.Background(), conn, map[string]any{"include_deps": true})
	if err != nil {
		t.Fatal(err)
	}
	apps := res.Extra["application"].([]any)
	entry := apps[0].(map[string]any)
	deps, ok := entry["dependencies"].([]any)
	if !ok || len(deps) != 1 || deps[0] != "virtualenv" {
		t.Fatalf("dependencies = %v", entry["dependencies"])
	}
}

func TestModulePipxInfoIncludeRaw(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxToxList},
	})
	res, err := modulePipxInfo(context.Background(), conn, map[string]any{"include_raw": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Extra["raw_output"]; !ok {
		t.Fatal("want raw_output when include_raw=true")
	}
	// Exactly one `pipx list` invocation, shared between the raw fetch
	// and the parsed application list.
	n := 0
	for _, c := range conn.Commands {
		if c == "USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("pipx list ran %d times, want 1", n)
	}
}

func TestModulePipxInfoEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.7.1\n"},
		"USE_EMOJI=0 PIPX_USE_EMOJI=0 pipx list --include-injected --json": {RC: 0, Stdout: pipxEmptyList},
	})
	res, err := modulePipxInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	apps := res.Extra["application"].([]any)
	if len(apps) != 0 {
		t.Fatalf("application = %v", apps)
	}
}

func TestModulePipxInfoOldVersionRejected(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pipx version": {RC: 0, Stdout: "1.0.0\n"},
	})
	if _, err := modulePipxInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for pipx < 1.7.0")
	}
}
