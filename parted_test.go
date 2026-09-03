package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const partedPrintOut = "BYT;\n" +
	"/dev/sdb:5368709120B:scsi:512:512:msdos:VMware Virtual disk;\n" +
	"1:1049kB:538MB:537MB:ext4::boot;\n" +
	"2:538MB:1074MB:536MB:::;\n"

func TestModulePartedInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"parted -s -m /dev/sdb unit KiB print": {RC: 0, Stdout: partedPrintOut},
	})
	res, err := moduleParted(context.Background(), conn, map[string]any{"device": "/dev/sdb"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	disk := res.Extra["disk"].(map[string]string)
	if disk["table"] != "msdos" || disk["path"] != "/dev/sdb" {
		t.Fatalf("disk = %+v", disk)
	}
	parts := res.Extra["partitions"].([]any)
	if len(parts) != 2 {
		t.Fatalf("partitions = %+v", parts)
	}
	p1 := parts[0].(map[string]any)
	if p1["num"] != 1 || p1["fstype"] != "ext4" {
		t.Fatalf("p1 = %+v", p1)
	}
	flags := p1["flags"].([]string)
	if len(flags) != 1 || flags[0] != "boot" {
		t.Fatalf("flags = %+v", flags)
	}
}

func TestModulePartedCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"parted -s -m /dev/sdb unit KiB print":                                {RC: 0, Stdout: partedPrintOut},
		"parted -s -a optimal /dev/sdb unit KiB mkpart primary ext4 0% 100% ": {RC: 0},
	})
	res, err := moduleParted(context.Background(), conn, map[string]any{
		"device": "/dev/sdb", "number": 3, "state": "present", "fs_type": "ext4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePartedAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"parted -s -m /dev/sdb unit KiB print": {RC: 0, Stdout: partedPrintOut},
		"parted -s /dev/sdb rm 1":              {RC: 0},
	})
	res, err := moduleParted(context.Background(), conn, map[string]any{
		"device": "/dev/sdb", "number": 1, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePartedAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"parted -s -m /dev/sdb unit KiB print": {RC: 0, Stdout: partedPrintOut},
	})
	res, err := moduleParted(context.Background(), conn, map[string]any{
		"device": "/dev/sdb", "number": 9, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModulePartedAddFlag(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"parted -s -m /dev/sdb unit KiB print": {RC: 0, Stdout: partedPrintOut},
		"parted -s /dev/sdb set 1 lvm on":      {RC: 0},
	})
	res, err := moduleParted(context.Background(), conn, map[string]any{
		"device": "/dev/sdb", "number": 1, "state": "present",
		"flags": []any{"boot", "lvm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	// "boot" already set: must not re-issue `set 1 boot on`.
	for _, c := range conn.Commands {
		if c == "parted -s /dev/sdb set 1 boot on" {
			t.Fatalf("unexpected re-set of already-present flag, commands = %v", conn.Commands)
		}
	}
}

func TestModulePartedRelabelWipes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"parted -s -m /dev/sdb unit KiB print":                           {RC: 0, Stdout: partedPrintOut},
		"parted -s /dev/sdb mklabel gpt":                                 {RC: 0},
		"parted -s -a optimal /dev/sdb unit KiB mkpart primary 0% 100% ": {RC: 0},
	})
	res, err := moduleParted(context.Background(), conn, map[string]any{
		"device": "/dev/sdb", "number": 1, "state": "present", "label": "gpt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "parted -s /dev/sdb mklabel gpt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want mklabel gpt issued, commands = %v", conn.Commands)
	}
}

func TestModulePartedResize(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"parted -s -m /dev/sdb unit KiB print": {RC: 0, Stdout: partedPrintOut},
		"parted -s /dev/sdb resizepart 1 100%": {RC: 0},
	})
	res, err := moduleParted(context.Background(), conn, map[string]any{
		"device": "/dev/sdb", "number": 1, "state": "present", "resize": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePartedMissingDevice(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleParted(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing device")
	}
}

func TestModulePartedMissingNumber(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"parted -s -m /dev/sdb unit KiB print": {RC: 0, Stdout: partedPrintOut},
	})
	if _, err := moduleParted(context.Background(), conn, map[string]any{"device": "/dev/sdb", "state": "present"}); err == nil {
		t.Fatal("want error for missing number")
	}
}
