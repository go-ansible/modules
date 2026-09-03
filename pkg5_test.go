package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePkg5Install(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg list -- editor/vim >/dev/null 2>&1": {RC: 1},
		"pkg install -q -- editor/vim":           {RC: 0},
	})
	res, err := modulePkg5(context.Background(), conn, map[string]any{"name": "editor/vim"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePkg5AlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg list -- editor/vim >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePkg5(context.Background(), conn, map[string]any{"name": "editor/vim"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePkg5AbsentWithFlags(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg list -- service/network/finger >/dev/null 2>&1":                 {RC: 0},
		"pkg uninstall --accept --be-name=next -q -- service/network/finger": {RC: 0},
	})
	res, err := modulePkg5(context.Background(), conn, map[string]any{
		"name": "service/network/finger", "state": "absent", "accept_licenses": true, "be_name": "next",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkg5NoRefreshVerbose(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg list -- editor/vim >/dev/null 2>&1": {RC: 1},
		"pkg install --no-refresh -- editor/vim": {RC: 0},
	})
	res, err := modulePkg5(context.Background(), conn, map[string]any{
		"name": "editor/vim", "refresh": false, "verbose": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkg5MultiplePackages(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pkg list -- /file/gnu-findutils >/dev/null 2>&1":      {RC: 1},
		"pkg list -- /text/gnu-grep >/dev/null 2>&1":           {RC: 1},
		"pkg install -q -- /file/gnu-findutils /text/gnu-grep": {RC: 0},
	})
	res, err := modulePkg5(context.Background(), conn, map[string]any{
		"name": []string{"/file/gnu-findutils", "/text/gnu-grep"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePkg5MissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePkg5(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
