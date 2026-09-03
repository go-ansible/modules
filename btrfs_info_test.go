package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const btrfsShowOut = "Label: 'Tank'  uuid: 96c9c605-1454-49b8-a63a-15e2584c208e\n" +
	"\tTotal devices 1 FS bytes used 10.00GiB\n" +
	"\tdevid    1 size 50.00GiB used 12.00GiB path /dev/sda1\n\n"

const procMountsOut = "/dev/sda1 / btrfs rw,relatime,subvolid=5,subvol=/ 0 0\n" +
	"/dev/sda1 /home btrfs rw,relatime,subvolid=256,subvol=/@home 0 0\n"

func TestModuleBtrfsInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null":         {RC: 0, Stdout: btrfsShowOut},
		"cat /proc/mounts 2>/dev/null":              {RC: 0, Stdout: procMountsOut},
		"btrfs subvolume get-default / 2>/dev/null": {RC: 0, Stdout: "ID 5 (FS_TREE)\n"},
		"btrfs subvolume list -a / 2>/dev/null":     {RC: 0, Stdout: "ID 256 gen 10 top level 5 path @home\n"},
	})
	res, err := moduleBtrfsInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("btrfs_info must never report changed/failed on success, res = %+v", res)
	}
	fss := res.Extra["filesystems"].([]any)
	if len(fss) != 1 {
		t.Fatalf("filesystems = %+v", fss)
	}
	fs := fss[0].(map[string]any)
	if fs["uuid"] != "96c9c605-1454-49b8-a63a-15e2584c208e" || fs["label"] != "Tank" {
		t.Fatalf("fs = %+v", fs)
	}
	if fs["default_subvolume"] != 5 {
		t.Fatalf("default_subvolume = %v", fs["default_subvolume"])
	}
	subvols := fs["subvolumes"].([]any)
	if len(subvols) != 1 {
		t.Fatalf("subvolumes = %+v", subvols)
	}
	sv := subvols[0].(map[string]any)
	if sv["path"] != "@home" {
		t.Fatalf("sv = %+v", sv)
	}
	mps := sv["mountpoints"].([]string)
	if len(mps) != 1 || mps[0] != "/home" {
		t.Fatalf("mountpoints = %+v", mps)
	}
}

func TestModuleBtrfsInfoUnmountedFilesystem(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null": {RC: 0, Stdout: btrfsShowOut},
		"cat /proc/mounts 2>/dev/null":      {RC: 0, Stdout: ""},
	})
	res, err := moduleBtrfsInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	fss := res.Extra["filesystems"].([]any)
	fs := fss[0].(map[string]any)
	if _, ok := fs["default_subvolume"]; ok {
		t.Fatalf("want no default_subvolume key when unmounted, fs = %+v", fs)
	}
	if _, ok := fs["subvolumes"]; ok {
		t.Fatalf("want no subvolumes key when unmounted, fs = %+v", fs)
	}
}

func TestModuleBtrfsInfoNoFilesystems(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null": {RC: 1},
		"cat /proc/mounts 2>/dev/null":      {RC: 0, Stdout: ""},
	})
	res, err := moduleBtrfsInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	fss := res.Extra["filesystems"].([]any)
	if len(fss) != 0 {
		t.Fatalf("filesystems = %+v", fss)
	}
}
