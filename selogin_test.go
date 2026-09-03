package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const seloginListSample = `Login Name           SELinux User         MLS/MCS Range        Service
__default__          unconfined_u         s0-s0:c0.c1023        *
root                 unconfined_u         s0-s0:c0.c1023        *
gijoe                staff_u              SystemLow-Secret      *
`

func TestModuleSeloginAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage login -l":                          {RC: 0, Stdout: seloginListSample},
		"semanage login -a -s guest_u -r s0 newuser": {RC: 0},
	})
	res, err := moduleSelogin(context.Background(), conn, map[string]any{
		"login": "newuser", "seuser": "guest_u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSeloginAlreadyMapped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage login -l": {RC: 0, Stdout: seloginListSample},
	})
	res, err := moduleSelogin(context.Background(), conn, map[string]any{
		"login": "gijoe", "seuser": "staff_u", "selevel": "SystemLow-Secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSeloginModify(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage login -l":                        {RC: 0, Stdout: seloginListSample},
		"semanage login -m -s staff_u -r s0 gijoe": {RC: 0},
	})
	res, err := moduleSelogin(context.Background(), conn, map[string]any{
		"login": "gijoe", "seuser": "staff_u",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed (selevel differs: s0 vs SystemLow-Secret)")
	}
}

func TestModuleSeloginDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage login -l":       {RC: 0, Stdout: seloginListSample},
		"semanage login -d gijoe": {RC: 0},
	})
	res, err := moduleSelogin(context.Background(), conn, map[string]any{
		"login": "gijoe", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSeloginDeleteAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage login -l": {RC: 0, Stdout: seloginListSample},
	})
	res, err := moduleSelogin(context.Background(), conn, map[string]any{
		"login": "nobody", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSeloginValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSelogin(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing login")
	}
	if _, err := moduleSelogin(context.Background(), conn, map[string]any{"login": "x"}); err == nil {
		t.Fatal("want error: seuser required when present")
	}
}
