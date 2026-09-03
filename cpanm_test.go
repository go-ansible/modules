package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleCpanmInstallViaProbe(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"perl -MDancer -e1 >/dev/null 2>&1": {RC: 1},
		"cpanm Dancer":                      {RC: 0, Stdout: "Successfully installed Dancer-1.0"},
	})
	res, err := moduleCpanm(context.Background(), conn, map[string]any{"name": "Dancer"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleCpanmAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"perl -MDancer -e1 >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleCpanm(context.Background(), conn, map[string]any{"name": "Dancer"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want only the probe", conn.Commands)
	}
}

func TestModuleCpanmUpToDateOutputParsing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cpanm 'Plack~1.0'": {RC: 0, Stdout: "Plack is up to date. (1.0000)"},
	})
	res, err := moduleCpanm(context.Background(), conn, map[string]any{
		"name":    "Plack",
		"version": "1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if !strings.Contains(res.Msg, "is up to date") {
		t.Fatalf("msg = %q", res.Msg)
	}
	// version set: no perl -M probe, straight to cpanm.
	if len(conn.Commands) != 1 || conn.Commands[0] != "cpanm 'Plack~1.0'" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleCpanmNameCheck(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"perl -MPlack -e1 >/dev/null 2>&1": {RC: 0},
	})
	res, err := moduleCpanm(context.Background(), conn, map[string]any{
		"name":       "MIYAGAWA/Plack-0.99_05.tar.gz",
		"name_check": "Plack",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleCpanmMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleCpanm(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleCpanmInvalidMode(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleCpanm(context.Background(), conn, map[string]any{
		"name": "Dancer",
		"mode": "compatibility",
	})
	if err == nil {
		t.Fatal("want error for removed compatibility mode")
	}
}
