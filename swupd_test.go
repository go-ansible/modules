package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSwupdInstallBundle(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /usr/share/clear/bundles/foo": {RC: 1},
		"swupd bundle-add foo":                 {RC: 0},
	})
	res, err := moduleSwupd(context.Background(), conn, map[string]any{"name": "foo", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSwupdRemoveBundle(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /usr/share/clear/bundles/foo": {RC: 0},
		"swupd bundle-remove foo":              {RC: 0},
	})
	res, err := moduleSwupd(context.Background(), conn, map[string]any{"name": "foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSwupdUpdateNoneAvailable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swupd check-update": {RC: 1},
	})
	res, err := moduleSwupd(context.Background(), conn, map[string]any{"update": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSwupdUpdateAvailable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swupd check-update": {RC: 0},
		"swupd update":       {RC: 0},
	})
	res, err := moduleSwupd(context.Background(), conn, map[string]any{"update": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSwupdVerifyNoIssues(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swupd verify": {RC: 0, Stdout: "no problems found\n"},
	})
	res, err := moduleSwupd(context.Background(), conn, map[string]any{"verify": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSwupdVerifyFixesIssues(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"swupd verify":       {RC: 0, Stdout: "files did not match\n"},
		"swupd verify --fix": {RC: 0, Stdout: "missing files were replaced\n"},
	})
	res, err := moduleSwupd(context.Background(), conn, map[string]any{"verify": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSwupdContenturlAlwaysAppended(t *testing.T) {
	// Real swupd's own module has a latent bug that appends
	// --contenturl to every sub-command including check-update; this
	// port reproduces it, so assert the exact command line.
	conn := newFakeConn(map[string]remoteexec.Result{
		"swupd check-update --contenturl=http://example.com": {RC: 1},
	})
	res, err := moduleSwupd(context.Background(), conn, map[string]any{
		"update": true, "contenturl": "http://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSwupdMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSwupd(context.Background(), conn, map[string]any{"update": true, "verify": true}); err == nil {
		t.Fatal("want error for mutually exclusive update+verify")
	}
}

func TestModuleSwupdRequiredOneOf(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSwupd(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when none of name/update/verify given")
	}
}
