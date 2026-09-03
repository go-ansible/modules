package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRhsmReleaseSet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                               {RC: 0, Stdout: "0\n"},
		"subscription-manager release --show": {RC: 0, Stdout: "Release: 7.2\n"},
		"subscription-manager release --set " + shellQuote("8.1"): {RC: 0},
	})
	res, err := moduleRhsmRelease(context.Background(), conn, map[string]any{"release": "8.1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["current_release"] != "8.1" {
		t.Fatalf("current_release = %v", res.Extra["current_release"])
	}
}

func TestModuleRhsmReleaseUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                               {RC: 0, Stdout: "0\n"},
		"subscription-manager release --show": {RC: 0, Stdout: "Release: 7.2\n"},
	})
	res, err := moduleRhsmRelease(context.Background(), conn, map[string]any{"release": "7.2"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRhsmReleaseUnset(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                                {RC: 0, Stdout: "0\n"},
		"subscription-manager release --show":  {RC: 0, Stdout: "Release: 7.2\n"},
		"subscription-manager release --unset": {RC: 0},
	})
	res, err := moduleRhsmRelease(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["current_release"] != "" {
		t.Fatalf("current_release = %v", res.Extra["current_release"])
	}
}

func TestModuleRhsmReleaseNotRoot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u": {RC: 0, Stdout: "1000\n"},
	})
	res, err := moduleRhsmRelease(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when not root")
	}
}

func TestModuleRhsmReleaseInvalidValue(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u": {RC: 0, Stdout: "0\n"},
	})
	res, err := moduleRhsmRelease(context.Background(), conn, map[string]any{"release": "not-a-release!!"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an invalid release value")
	}
}
