package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHponcfgBasic(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"hponcfg -f /tmp/enable-ssh.xml": {RC: 0, Stdout: "<RIBCL VERSION=\"2.0\">\n</RIBCL>"},
	})
	args := map[string]any{"path": "/tmp/enable-ssh.xml"}
	res, err := moduleHponcfg(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHponcfgVerboseAndMinfwAndExecutable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"/opt/hp/tools/hponcfg -f /tmp/enable-ssh.xml -v -m 2.10": {RC: 0},
	})
	args := map[string]any{
		"path": "/tmp/enable-ssh.xml", "verbose": true, "minfw": "2.10",
		"executable": "/opt/hp/tools/hponcfg",
	}
	res, err := moduleHponcfg(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHponcfgFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"hponcfg -f /tmp/bad.xml": {RC: 1, Stderr: "Error: malformed XML"},
	})
	args := map[string]any{"path": "/tmp/bad.xml"}
	res, err := moduleHponcfg(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleHponcfgMissingPath(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	_, err := moduleHponcfg(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing path")
	}
}
