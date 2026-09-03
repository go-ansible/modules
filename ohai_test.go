package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOhaiTopLevelExtra(t *testing.T) {
	getCmd := "env LANGUAGE=C LC_ALL=C ohai"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ohai": {RC: 0},
		getCmd:            {RC: 0, Stdout: `{"platform":"ubuntu","kernel":{"name":"Linux"}}`},
	})
	res, err := moduleOhai(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["platform"] != "ubuntu" {
		t.Fatalf("Extra[platform] = %v, want ohai's own top-level keys spread into Extra", res.Extra["platform"])
	}
	if res.Facts != nil {
		t.Fatalf("Facts = %+v, want nil: ohai's own data is NOT merged into ansible_facts", res.Facts)
	}
}

func TestModuleOhaiNoBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ohai": {RC: 1},
	})
	res, err := moduleOhai(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleOhaiCommandFails(t *testing.T) {
	getCmd := "env LANGUAGE=C LC_ALL=C ohai"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ohai": {RC: 0},
		getCmd:            {RC: 1, Stderr: "boom"},
	})
	res, err := moduleOhai(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
