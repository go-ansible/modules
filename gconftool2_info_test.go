package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleGconftool2InfoValue(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v gconftool-2": {RC: 0},
		"gconftool-2 --version":  {RC: 0, Stdout: "3.2.6\n"},
		"gconftool-2 --get /desktop/gnome/background/picture_filename": {RC: 0, Stdout: "Monospace 10\n"},
	})
	res, err := moduleGconftool2Info(context.Background(), fc, map[string]any{
		"key": "/desktop/gnome/background/picture_filename",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["value"] != "Monospace 10" {
		t.Fatalf("value = %v", res.Extra["value"])
	}
	if res.Extra["version"] != "3.2.6" {
		t.Fatalf("version = %v", res.Extra["version"])
	}
}

func TestModuleGconftool2InfoNoValueWithStderr(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v gconftool-2":     {RC: 0},
		"gconftool-2 --version":      {RC: 0, Stdout: "3.2.6\n"},
		"gconftool-2 --get /foo/bar": {RC: 0, Stdout: "", Stderr: "No value set for `/foo/bar'\n"},
	})
	res, err := moduleGconftool2Info(context.Background(), fc, map[string]any{"key": "/foo/bar"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != nil {
		t.Fatalf("value = %v, want nil", res.Extra["value"])
	}
}

func TestModuleGconftool2InfoStdoutWinsOverStderr(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v gconftool-2":     {RC: 0},
		"gconftool-2 --version":      {RC: 0, Stdout: "3.2.6\n"},
		"gconftool-2 --get /foo/bar": {RC: 0, Stdout: "value-despite-warning\n", Stderr: "some warning\n"},
	})
	res, err := moduleGconftool2Info(context.Background(), fc, map[string]any{"key": "/foo/bar"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Extra["value"] != "value-despite-warning" {
		t.Fatalf("value = %v, want the stdout even with stderr present", res.Extra["value"])
	}
}

func TestModuleGconftool2InfoFails(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v gconftool-2":     {RC: 0},
		"gconftool-2 --version":      {RC: 0, Stdout: "3.2.6\n"},
		"gconftool-2 --get /foo/bar": {RC: 1, Stderr: "boom"},
	})
	res, err := moduleGconftool2Info(context.Background(), fc, map[string]any{"key": "/foo/bar"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed on a non-zero exit")
	}
}

func TestModuleGconftool2InfoMissingKey(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleGconftool2Info(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing key")
	}
}
