package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestParseProcMounts(t *testing.T) {
	out := "/dev/sda1 / ext4 rw,relatime 0 0\nnone /proc proc rw,nosuid 0 0\n"
	mounts := parseProcMounts(out)
	if len(mounts) != 2 {
		t.Fatalf("mounts = %#v", mounts)
	}
	if mounts[0]["device"] != "/dev/sda1" || mounts[0]["mount_point"] != "/" || mounts[0]["fstype"] != "ext4" {
		t.Fatalf("mounts[0] = %#v", mounts[0])
	}
	opts := mounts[0]["options"].([]string)
	if len(opts) != 2 || opts[0] != "rw" || opts[1] != "relatime" {
		t.Fatalf("options = %#v", opts)
	}
}

func TestParseMountCommandBSD(t *testing.T) {
	out := "/dev/disk1s1 on / (apfs, local, journaled)\n"
	mounts := parseMountCommand(out)
	if len(mounts) != 1 {
		t.Fatalf("mounts = %#v", mounts)
	}
	if mounts[0]["device"] != "/dev/disk1s1" || mounts[0]["mount_point"] != "/" || mounts[0]["fstype"] != "apfs" {
		t.Fatalf("mounts[0] = %#v", mounts[0])
	}
	opts := mounts[0]["options"].([]string)
	if len(opts) != 2 || opts[0] != "local" || opts[1] != "journaled" {
		t.Fatalf("options = %#v", opts)
	}
}

func TestParseMountCommandLinux(t *testing.T) {
	out := "/dev/sda1 on / type ext4 (rw,relatime)\n"
	mounts := parseMountCommand(out)
	if len(mounts) != 1 {
		t.Fatalf("mounts = %#v", mounts)
	}
	if mounts[0]["fstype"] != "ext4" || mounts[0]["mount_point"] != "/" {
		t.Fatalf("mounts[0] = %#v", mounts[0])
	}
	opts := mounts[0]["options"].([]string)
	if len(opts) != 2 || opts[0] != "rw" || opts[1] != "relatime" {
		t.Fatalf("options = %#v", opts)
	}
}

func TestMatchesAny(t *testing.T) {
	if !matchesAny([]string{"/dev/sd*"}, "/dev/sda1") {
		t.Fatal("want match")
	}
	if matchesAny([]string{"/dev/nvme*"}, "/dev/sda1") {
		t.Fatal("want no match")
	}
}

func TestModuleMountFactsProcMounts(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /proc/mounts 2>/dev/null": {RC: 0, Stdout: "/dev/sda1 / ext4 rw 0 0\n"},
	})
	res, err := moduleMountFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	mounts := res.Extra["mounts"].([]map[string]any)
	if len(mounts) != 1 {
		t.Fatalf("mounts = %#v", mounts)
	}
}

func TestModuleMountFactsFallsBackToMount(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /proc/mounts 2>/dev/null": {RC: 1},
		"mount":                        {RC: 0, Stdout: "/dev/disk1s1 on / (apfs, local)\n"},
	})
	res, err := moduleMountFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	mounts := res.Extra["mounts"].([]map[string]any)
	if len(mounts) != 1 || mounts[0]["fstype"] != "apfs" {
		t.Fatalf("mounts = %#v", mounts)
	}
}

func TestModuleMountFactsNoSourceWorks(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /proc/mounts 2>/dev/null": {RC: 1},
		"mount":                        {RC: 1},
	})
	if _, err := moduleMountFacts(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when no mount source works")
	}
}

func TestModuleMountFactsFilters(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /proc/mounts 2>/dev/null": {RC: 0, Stdout: "/dev/sda1 / ext4 rw 0 0\nnone /proc proc rw 0 0\n"},
	})
	res, err := moduleMountFacts(context.Background(), conn, map[string]any{"fstypes": []string{"ext*"}})
	if err != nil {
		t.Fatal(err)
	}
	mounts := res.Extra["mounts"].([]map[string]any)
	if len(mounts) != 1 || mounts[0]["fstype"] != "ext4" {
		t.Fatalf("mounts = %#v", mounts)
	}
}
