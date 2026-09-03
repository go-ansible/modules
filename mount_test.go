package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleMountPresentNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null": {RC: 0, Stdout: "/dev/sda1 / ext4 defaults 0 1\n"},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "src": "/dev/sdb1", "fstype": "ext4", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	last := conn.Stdins[len(conn.Stdins)-1]
	want := "/dev/sda1 / ext4 defaults 0 1\n/dev/sdb1 /mnt/data ext4 defaults 0 0\n"
	if last != want {
		t.Fatalf("written fstab = %q, want %q", last, want)
	}
}

func TestModuleMountPresentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null": {RC: 0, Stdout: "/dev/sdb1 /mnt/data ext4 defaults 0 0\n"},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "src": "/dev/sdb1", "fstype": "ext4", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMountPresentNoAuto(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null": {RC: 1},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "src": "/dev/sdb1", "fstype": "ext4", "boot": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	want := "/dev/sdb1 /mnt/data ext4 defaults,noauto 0 0\n"
	if last := conn.Stdins[len(conn.Stdins)-1]; last != want {
		t.Fatalf("written fstab = %q, want %q", last, want)
	}
}

func TestModuleMountMountedAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null":   {RC: 0, Stdout: "/dev/sdb1 /mnt/data ext4 defaults 0 0\n"},
		"mkdir -p /mnt/data":           {RC: 0},
		"cat /proc/mounts 2>/dev/null": {RC: 0, Stdout: "/dev/sdb1 /mnt/data ext4 rw,defaults 0 0\n"},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "src": "/dev/sdb1", "fstype": "ext4", "state": "mounted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMountMountedNeedsMount(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null":   {RC: 1},
		"mkdir -p /mnt/data":           {RC: 0},
		"cat /proc/mounts 2>/dev/null": {RC: 0, Stdout: ""},
		"mount":                        {RC: 0, Stdout: ""},
		"mount /mnt/data":              {RC: 0},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "src": "/dev/sdb1", "fstype": "ext4", "state": "mounted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMountUnmounted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /proc/mounts 2>/dev/null": {RC: 0, Stdout: "/dev/sdb1 /mnt/data ext4 rw 0 0\n"},
		"umount /mnt/data":             {RC: 0},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "state": "unmounted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMountUnmountedAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /proc/mounts 2>/dev/null": {RC: 0, Stdout: ""},
		"mount":                        {RC: 0, Stdout: ""},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "state": "unmounted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMountAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null":   {RC: 0, Stdout: "/dev/sdb1 /mnt/data ext4 defaults 0 0\n"},
		"cat /proc/mounts 2>/dev/null": {RC: 0, Stdout: "/dev/sdb1 /mnt/data ext4 rw 0 0\n"},
		"umount /mnt/data":             {RC: 0},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMountAbsentFromFstabOnly(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null": {RC: 0, Stdout: "/dev/sdb1 /mnt/data ext4 defaults 0 0\n"},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "state": "absent_from_fstab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	for _, c := range conn.Commands {
		if c == "umount /mnt/data" {
			t.Fatal("absent_from_fstab must not unmount")
		}
	}
}

func TestModuleMountAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null": {RC: 1},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "state": "absent_from_fstab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleMountRemounted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"mount -o remount /mnt/data": {RC: 0},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "state": "remounted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleMountEphemeralUnimplemented(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "state": "ephemeral",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}

func TestModuleMountBackup(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/fstab 2>/dev/null": {RC: 1},
	})
	res, err := moduleMount(context.Background(), conn, map[string]any{
		"path": "/mnt/data", "src": "/dev/sdb1", "fstype": "ext4", "backup": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "cp /etc/fstab /etc/fstab.$(date +%Y%m%d%H%M%S) 2>/dev/null" {
			found = true
		}
	}
	if !found {
		t.Fatalf("backup command not issued: %v", conn.Commands)
	}
}

func TestModuleMountValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleMount(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing path")
	}
	if _, err := moduleMount(context.Background(), conn, map[string]any{"path": "/mnt/x"}); err == nil {
		t.Fatal("want error for missing src (state=present)")
	}
	if _, err := moduleMount(context.Background(), conn, map[string]any{"path": "/mnt/x", "src": "/dev/x"}); err == nil {
		t.Fatal("want error for missing fstype")
	}
	if _, err := moduleMount(context.Background(), conn, map[string]any{"path": "/mnt/x", "state": "bogus"}); err == nil {
		t.Fatal("want error for unknown state")
	}
}
