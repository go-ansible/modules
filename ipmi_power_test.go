package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const ipmiPowerBase = "ipmitool -I lanplus -H test.testdomain.com -p 623 -U admin -P password"

func TestModuleIPMIPowerOnFromOff(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		ipmiPowerBase + " chassis power status": {RC: 0, Stdout: "Chassis Power is off\n"},
		ipmiPowerBase + " chassis power on":     {RC: 0},
	})
	res, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password", "state": "on",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["powerstate"] != "on" {
		t.Fatalf("powerstate = %v", res.Extra["powerstate"])
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleIPMIPowerOnAlreadyOn(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		ipmiPowerBase + " chassis power status": {RC: 0, Stdout: "Chassis Power is on\n"},
	})
	res, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password", "state": "on",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want only the status probe", conn.Commands)
	}
}

func TestModuleIPMIPowerBootFromOff(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		ipmiPowerBase + " chassis power status": {RC: 0, Stdout: "Chassis Power is off\n"},
		ipmiPowerBase + " chassis power on":     {RC: 0},
	})
	res, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password", "state": "boot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[1] != ipmiPowerBase+" chassis power on" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleIPMIPowerBootFromOn(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		ipmiPowerBase + " chassis power status": {RC: 0, Stdout: "Chassis Power is on\n"},
		ipmiPowerBase + " chassis power reset":  {RC: 0},
	})
	res, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password", "state": "boot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[1] != ipmiPowerBase+" chassis power reset" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleIPMIPowerShutdownAlwaysApplied(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		ipmiPowerBase + " chassis power status": {RC: 0, Stdout: "Chassis Power is on\n"},
		ipmiPowerBase + " chassis power soft":   {RC: 0},
	})
	res, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password", "state": "shutdown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: shutdown/reset/boot never compare equal to the on/off status probe")
	}
	if res.Extra["powerstate"] != "shutdown" {
		t.Fatalf("powerstate = %v", res.Extra["powerstate"])
	}
}

func TestModuleIPMIPowerMachineList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		ipmiPowerBase + " -t 48 chassis power status": {RC: 0, Stdout: "Chassis Power is on\n"},
		ipmiPowerBase + " -t 48 chassis power off":    {RC: 0},
		ipmiPowerBase + " -t 50 chassis power status": {RC: 0, Stdout: "Chassis Power is off\n"},
		ipmiPowerBase + " -t 50 chassis power on":     {RC: 0},
	})
	res, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password",
		"state": "on",
		"machine": []any{
			map[string]any{"targetAddress": 48, "state": "off"},
			map[string]any{"targetAddress": 50},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	status, _ := res.Extra["status"].([]map[string]any)
	if len(status) != 2 {
		t.Fatalf("status = %v", res.Extra["status"])
	}
	if status[0]["targetAddress"] != 48 || status[0]["powerstate"] != "off" {
		t.Fatalf("status[0] = %v", status[0])
	}
	if status[1]["targetAddress"] != 50 || status[1]["powerstate"] != "on" {
		t.Fatalf("status[1] = %v", status[1])
	}
}

func TestModuleIPMIPowerTargetAddressOutOfRange(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password",
		"machine": []any{map[string]any{"targetAddress": 300, "state": "on"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an out-of-range targetAddress")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("expected no ipmitool invocation, got %v", conn.Commands)
	}
}

func TestModuleIPMIPowerMissingStateAndMachine(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password",
	}); err == nil {
		t.Fatal("want error when neither state nor machine is given")
	}
}

func TestModuleIPMIPowerBadState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleIPMIPowerNonZero(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		ipmiPowerBase + " chassis power status": {RC: 0, Stdout: "Chassis Power is off\n"},
		ipmiPowerBase + " chassis power on":     {RC: 1, Stderr: "Unable to establish IPMI"},
	})
	res, err := moduleIPMIPower(context.Background(), conn, map[string]any{
		"name": "test.testdomain.com", "user": "admin", "password": "password", "state": "on",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-zero exit")
	}
}
