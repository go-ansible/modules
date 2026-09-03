package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKrbTicketPresentObtains(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"klist": {RC: 1},
		"kinit": {RC: 0},
	})
	res, err := moduleKrbTicket(context.Background(), conn, map[string]any{"password": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	for _, stdin := range conn.Stdins {
		if stdin == "secret\n" {
			return
		}
	}
	t.Fatal("password was not piped over stdin")
}

func TestModuleKrbTicketPasswordNotOnCommandLine(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"klist": {RC: 1},
		"kinit": {RC: 0},
	})
	if _, err := moduleKrbTicket(context.Background(), conn, map[string]any{"password": "topsecret"}); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range conn.Commands {
		if cmd != "kinit" && cmd != "klist" {
			t.Fatalf("unexpected command %q — password must never be a command-line argument", cmd)
		}
	}
}

func TestModuleKrbTicketAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"klist": {RC: 0},
	})
	res, err := moduleKrbTicket(context.Background(), conn, map[string]any{"password": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	for _, cmd := range conn.Commands {
		if cmd == "kinit" {
			t.Fatal("kinit should not have been run")
		}
	}
}

func TestModuleKrbTicketKinitFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"klist": {RC: 1},
		"kinit": {RC: 1, Stderr: "kinit: Password incorrect while getting initial credentials"},
	})
	res, err := moduleKrbTicket(context.Background(), conn, map[string]any{"password": "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed")
	}
}

func TestModuleKrbTicketAbsentDestroys(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"klist":    {RC: 0},
		"kdestroy": {RC: 0},
	})
	res, err := moduleKrbTicket(context.Background(), conn, map[string]any{"state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKrbTicketAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"klist": {RC: 1},
	})
	res, err := moduleKrbTicket(context.Background(), conn, map[string]any{"state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleKrbTicketKdestroyAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"klist":       {RC: 1},
		"kdestroy -A": {RC: 0},
	})
	res, err := moduleKrbTicket(context.Background(), conn, map[string]any{"state": "absent", "kdestroy_all": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleKrbTicketPresentRequiresPasswordOrKeytab(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKrbTicket(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when neither password nor keytab_path is given")
	}
}

func TestModuleKrbTicketKeytabPathRequiresKeytab(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKrbTicket(context.Background(), conn, map[string]any{"keytab_path": "/etc/krb5.keytab"}); err == nil {
		t.Fatal("want error when keytab_path is given without keytab=true")
	}
}

func TestModuleKrbTicketKeytab(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"klist":                            {RC: 1},
		"kinit -k -t /etc/ipa/file.keytab": {RC: 0},
	})
	res, err := moduleKrbTicket(context.Background(), conn, map[string]any{
		"keytab": true, "keytab_path": "/etc/ipa/file.keytab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
