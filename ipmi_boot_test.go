package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIPMIBootBasic(t *testing.T) {
	want := "ipmitool -I lanplus -H test.testdomain.com -p 623 -U admin -P password chassis bootdev disk"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleIPMIBoot(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password", "bootdev": "hd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["bootdev"] != "hd" || res.Extra["persistent"] != false || res.Extra["uefimode"] != false {
		t.Fatalf("extra = %+v", res.Extra)
	}
	if len(conn.Commands) != 1 || conn.Commands[0] != want {
		t.Fatalf("commands = %v, want [%q]", conn.Commands, want)
	}
}

func TestModuleIPMIBootAbsentWithKeyAndNetwork(t *testing.T) {
	want := "ipmitool -I lanplus -H test.testdomain.com -p 623 -U admin -P password" +
		" -y 1234567890AABBCCDEFF000000EEEE12 chassis bootdev none"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleIPMIBoot(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password",
		"key": "1234567890AABBCCDEFF000000EEEE12", "bootdev": "network", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleIPMIBootPersistentAndUefi(t *testing.T) {
	want := "ipmitool -I lanplus -H bmc -p 623 -U admin -P pw chassis bootdev pxe options=persistent,efiboot"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 0}})
	res, err := moduleIPMIBoot(context.Background(), conn, map[string]any{
		"name": "bmc", "user": "admin", "password": "pw", "bootdev": "network",
		"persistent": true, "uefiboot": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[0] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[0], want)
	}
}

func TestModuleIPMIBootAbsentDefaultRejected(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleIPMIBoot(context.Background(), conn, map[string]any{
		"name": "bmc", "user": "admin", "password": "pw", "bootdev": "default", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for state=absent with bootdev=default")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("expected no ipmitool invocation, got %v", conn.Commands)
	}
}

func TestModuleIPMIBootBadBootdev(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIPMIBoot(context.Background(), conn, map[string]any{
		"name": "bmc", "user": "admin", "password": "pw", "bootdev": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid bootdev")
	}
}

func TestModuleIPMIBootMissingRequired(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIPMIBoot(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}

func TestModuleIPMIBootNonZero(t *testing.T) {
	want := "ipmitool -I lanplus -H bmc -p 623 -U admin -P pw chassis bootdev disk"
	conn := newFakeConn(map[string]remoteexec.Result{want: {RC: 1, Stderr: "Unable to establish IPMI"}})
	res, err := moduleIPMIBoot(context.Background(), conn, map[string]any{
		"name": "bmc", "user": "admin", "password": "pw", "bootdev": "hd",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-zero exit")
	}
}
