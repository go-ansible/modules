package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleUsbFacts(t *testing.T) {
	lsusbOut := "Bus 001 Device 002: ID 1d6b:0002 Linux Foundation 2.0 root hub\n" +
		"Bus 002 Device 003: ID 046d:c52b Logitech, Inc. Unifying Receiver\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v lsusb >/dev/null 2>&1": {RC: 0},
		"lsusb":                            {RC: 0, Stdout: lsusbOut},
	})
	res, err := moduleUsbFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	devices, ok := res.Extra["usb_devices"].([]map[string]any)
	if !ok || len(devices) != 2 {
		t.Fatalf("usb_devices = %#v", res.Extra["usb_devices"])
	}
	if devices[0]["bus"] != "001" || devices[0]["device"] != "002" || devices[0]["id"] != "1d6b:0002" ||
		devices[0]["name"] != "Linux Foundation 2.0 root hub" {
		t.Fatalf("devices[0] = %#v", devices[0])
	}
	if devices[1]["name"] != "Logitech, Inc. Unifying Receiver" {
		t.Fatalf("devices[1] = %#v", devices[1])
	}
}

func TestModuleUsbFactsMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v lsusb >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleUsbFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed when lsusb missing")
	}
}

func TestParseLsusbEmpty(t *testing.T) {
	devices := parseLsusb("")
	if len(devices) != 0 {
		t.Fatalf("devices = %#v", devices)
	}
}
