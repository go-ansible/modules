package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const opendjConnFlags = "-h localhost --port 4444 --bindDN 'cn=Directory Manager' -j /tmp/opendj-backendprop-pw --backend-name userRoot"

func opendjArgs(extra map[string]any) map[string]any {
	base := map[string]any{
		"hostname": "localhost", "port": "4444", "backend": "userRoot",
		"name": "index-entry-limit", "value": "5000", "password": "secret",
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func TestModuleOpendjBackendpropSet(t *testing.T) {
	getCmd := "/opt/opendj/bin/dsconfig get-backend-prop -n -X -s " + opendjConnFlags
	setCmd := "/opt/opendj/bin/dsconfig set-backend-prop -n -X " + opendjConnFlags + " --set index-entry-limit:5000"
	fc := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 0, Stdout: "index-entry-limit    4000\n"},
		setCmd: {RC: 0},
	})
	res, err := moduleOpendjBackendprop(context.Background(), fc, opendjArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if fc.Stdins[0] != "secret" {
		t.Fatalf("temp password file content = %q", fc.Stdins[0])
	}
}

func TestModuleOpendjBackendpropAlreadySet(t *testing.T) {
	getCmd := "/opt/opendj/bin/dsconfig get-backend-prop -n -X -s " + opendjConnFlags
	fc := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 0, Stdout: "index-entry-limit    5000\n"},
	})
	res, err := moduleOpendjBackendprop(context.Background(), fc, opendjArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when the property already has the requested value")
	}
}

func TestModuleOpendjBackendpropEmptyGetIsUnchanged(t *testing.T) {
	getCmd := "/opt/opendj/bin/dsconfig get-backend-prop -n -X -s " + opendjConnFlags
	fc := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 0, Stdout: ""},
	})
	res, err := moduleOpendjBackendprop(context.Background(), fc, opendjArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: real opendj_backendprop never sets when get-backend-prop returns nothing (a reproduced quirk)")
	}
	for _, c := range fc.Commands {
		if strings.Contains(c, "set-backend-prop") {
			t.Fatalf("commands = %v, want no set-backend-prop call", fc.Commands)
		}
	}
}

func TestModuleOpendjBackendpropPasswordfile(t *testing.T) {
	connFlags := "-h localhost --port 4444 --bindDN 'cn=Directory Manager' -j /etc/opendj/pwfile --backend-name userRoot"
	getCmd := "/opt/opendj/bin/dsconfig get-backend-prop -n -X -s " + connFlags
	fc := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 0, Stdout: "index-entry-limit    5000\n"},
	})
	args := opendjArgs(map[string]any{"passwordfile": "/etc/opendj/pwfile"})
	delete(args, "password")
	res, err := moduleOpendjBackendprop(context.Background(), fc, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(fc.Commands) != 1 {
		t.Fatalf("commands = %v, want exactly one (no temp password file write)", fc.Commands)
	}
}

func TestModuleOpendjBackendpropMutuallyExclusive(t *testing.T) {
	fc := newFakeConn(nil)
	args := opendjArgs(map[string]any{"passwordfile": "/etc/opendj/pwfile"}) // both password and passwordfile
	if _, err := moduleOpendjBackendprop(context.Background(), fc, args); err == nil {
		t.Fatal("want error when both password and passwordfile are given")
	}

	args2 := opendjArgs(nil)
	delete(args2, "password")
	if _, err := moduleOpendjBackendprop(context.Background(), fc, args2); err == nil {
		t.Fatal("want error when neither password nor passwordfile is given")
	}
}

func TestModuleOpendjBackendpropGetFails(t *testing.T) {
	getCmd := "/opt/opendj/bin/dsconfig get-backend-prop -n -X -s " + opendjConnFlags
	fc := newFakeConn(map[string]remoteexec.Result{
		getCmd: {RC: 1, Stderr: "unable to connect"},
	})
	res, err := moduleOpendjBackendprop(context.Background(), fc, opendjArgs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when get-backend-prop itself fails")
	}
}

func TestModuleOpendjBackendpropMissingArgs(t *testing.T) {
	fc := newFakeConn(nil)
	args := opendjArgs(nil)
	delete(args, "hostname")
	if _, err := moduleOpendjBackendprop(context.Background(), fc, args); err == nil {
		t.Fatal("want error for missing hostname")
	}
}
