package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSelinuxPermissiveAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage permissive -n -l":      {RC: 0, Stdout: "abrt_t\ninit_t\n"},
		"semanage permissive -a httpd_t": {RC: 0},
	})
	res, err := moduleSelinuxPermissive(context.Background(), conn, map[string]any{
		"domain": "httpd_t", "permissive": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSelinuxPermissiveAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage permissive -n -l": {RC: 0, Stdout: "abrt_t\nhttpd_t\n"},
	})
	res, err := moduleSelinuxPermissive(context.Background(), conn, map[string]any{
		"name": "httpd_t", "permissive": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSelinuxPermissiveRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage permissive -n -l":      {RC: 0, Stdout: "abrt_t\nhttpd_t\n"},
		"semanage permissive -d httpd_t": {RC: 0},
	})
	res, err := moduleSelinuxPermissive(context.Background(), conn, map[string]any{
		"domain": "httpd_t", "permissive": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSelinuxPermissiveRemoveAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage permissive -n -l": {RC: 0, Stdout: "abrt_t\n"},
	})
	res, err := moduleSelinuxPermissive(context.Background(), conn, map[string]any{
		"domain": "httpd_t", "permissive": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSelinuxPermissiveStoreAndNoReload(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage permissive -n -l -S mystore":         {RC: 0, Stdout: ""},
		"semanage permissive -S mystore -a httpd_t -N": {RC: 0},
	})
	res, err := moduleSelinuxPermissive(context.Background(), conn, map[string]any{
		"domain": "httpd_t", "permissive": true, "store": "mystore", "no_reload": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSelinuxPermissiveValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSelinuxPermissive(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing domain")
	}
	if _, err := moduleSelinuxPermissive(context.Background(), conn, map[string]any{"domain": "x"}); err == nil {
		t.Fatal("want error for missing permissive")
	}
}
