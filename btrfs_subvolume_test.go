package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const btrfsSingleShowOut = "Label: 'Tank'  uuid: 96c9c605-1454-49b8-a63a-15e2584c208e\n" +
	"\tTotal devices 1 FS bytes used 10.00GiB\n" +
	"\tdevid    1 size 50.00GiB used 12.00GiB path /dev/vda2\n\n"

const btrfsRootOnlyMountsOut = "/dev/vda2 / btrfs rw,relatime 0 0\n"

func TestModuleBtrfsSubvolumeCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null":           {RC: 0, Stdout: btrfsSingleShowOut},
		"cat /proc/mounts 2>/dev/null":                {RC: 0, Stdout: btrfsRootOnlyMountsOut},
		"btrfs subvolume show /@home >/dev/null 2>&1": {RC: 1},
		"test -e /@home":                              {RC: 1},
		"btrfs subvolume create /@home":               {RC: 0},
		"btrfs subvolume show /@home 2>/dev/null":     {RC: 0, Stdout: "Subvolume ID:\t257\n"},
	})
	res, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{
		"name": "/@home", "filesystem_device": "/dev/vda2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["target_subvolume_id"] != 257 {
		t.Fatalf("target_subvolume_id = %v", res.Extra["target_subvolume_id"])
	}
}

func TestModuleBtrfsSubvolumeAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null":           {RC: 0, Stdout: btrfsSingleShowOut},
		"cat /proc/mounts 2>/dev/null":                {RC: 0, Stdout: btrfsRootOnlyMountsOut},
		"btrfs subvolume show /@home >/dev/null 2>&1": {RC: 0},
		"btrfs subvolume show /@home 2>/dev/null":     {RC: 0, Stdout: "Subvolume ID:\t257\n"},
	})
	res, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{
		"name": "/@home", "filesystem_device": "/dev/vda2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleBtrfsSubvolumeSnapshot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null":       {RC: 0, Stdout: btrfsSingleShowOut},
		"cat /proc/mounts 2>/dev/null":            {RC: 0, Stdout: btrfsRootOnlyMountsOut},
		"btrfs subvolume show /@ >/dev/null 2>&1": {RC: 1},
		"test -e /@":                                {RC: 1},
		"btrfs subvolume snapshot / /@":             {RC: 0},
		"btrfs subvolume show /@ 2>/dev/null":       {RC: 0, Stdout: "Subvolume ID:\t258\n"},
		"btrfs subvolume get-default / 2>/dev/null": {RC: 0, Stdout: "ID 5 (FS_TREE)\n"},
		"btrfs subvolume set-default 258 /":         {RC: 0},
	})
	res, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{
		"name": "/@", "snapshot_source": "/", "default": true, "filesystem_device": "/dev/vda2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleBtrfsSubvolumeSnapshotConflictSkip(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null":       {RC: 0, Stdout: btrfsSingleShowOut},
		"cat /proc/mounts 2>/dev/null":            {RC: 0, Stdout: btrfsRootOnlyMountsOut},
		"btrfs subvolume show /@ >/dev/null 2>&1": {RC: 0},
		"btrfs subvolume show /@ 2>/dev/null":     {RC: 0, Stdout: "Subvolume ID:\t258\n"},
	})
	res, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{
		"name": "/@", "snapshot_source": "/", "filesystem_device": "/dev/vda2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want no-op for snapshot_conflict=skip on existing target, res = %+v", res)
	}
}

func TestModuleBtrfsSubvolumeAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null":           {RC: 0, Stdout: btrfsSingleShowOut},
		"cat /proc/mounts 2>/dev/null":                {RC: 0, Stdout: btrfsRootOnlyMountsOut},
		"btrfs subvolume show /@home >/dev/null 2>&1": {RC: 0},
		"btrfs subvolume delete /@home":               {RC: 0},
	})
	res, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{
		"name": "/@home", "state": "absent", "filesystem_device": "/dev/vda2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleBtrfsSubvolumeAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null":           {RC: 0, Stdout: btrfsSingleShowOut},
		"cat /proc/mounts 2>/dev/null":                {RC: 0, Stdout: btrfsRootOnlyMountsOut},
		"btrfs subvolume show /@home >/dev/null 2>&1": {RC: 1},
	})
	res, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{
		"name": "/@home", "state": "absent", "filesystem_device": "/dev/vda2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleBtrfsSubvolumeNoAutomountFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null": {RC: 0, Stdout: btrfsSingleShowOut},
		"cat /proc/mounts 2>/dev/null":      {RC: 0, Stdout: ""},
	})
	res, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{
		"name": "/@home", "filesystem_device": "/dev/vda2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed when no root mount and automount=false, res = %+v", res)
	}
}

func TestModuleBtrfsSubvolumeAmbiguousFilesystem(t *testing.T) {
	twoFS := btrfsSingleShowOut + "Label: none  uuid: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee\n" +
		"\tTotal devices 1 FS bytes used 1.00GiB\n" +
		"\tdevid    1 size 10.00GiB used 1.00GiB path /dev/vdb1\n\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"btrfs filesystem show 2>/dev/null": {RC: 0, Stdout: twoFS},
	})
	res, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{"name": "/@home"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed for ambiguous filesystem selection, res = %+v", res)
	}
}

func TestModuleBtrfsSubvolumeMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleBtrfsSubvolume(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
