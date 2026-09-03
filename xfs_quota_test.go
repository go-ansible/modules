package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXfsQuotaSetProjectDefaultLimits(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awk -v mp=/opt '$2 == mp && $3 == \"xfs\" {print $4}' /proc/mounts":   {RC: 0, Stdout: "rw,pquota\n"},
		"xfs_quota -x -c 'report -p -b' /opt":                                  {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'report -p -i' /opt":                                  {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'report -p -r' /opt":                                  {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'limit -p -d bsoft=1073741824 bhard=1073741824' /opt": {RC: 0},
	})
	res, err := moduleXfsQuota(context.Background(), conn, map[string]any{
		"type": "project", "mountpoint": "/opt", "bsoft": "1g", "bhard": "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if res.Extra["bsoft"] != int64(1073741824) {
		t.Fatalf("res.Extra = %v", res.Extra)
	}
}

func TestModuleXfsQuotaAbsentSetsToZero(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awk -v mp=/opt '$2 == mp && $3 == \"xfs\" {print $4}' /proc/mounts": {RC: 0, Stdout: "rw,pquota\n"},
		"xfs_quota -x -c 'report -p -b' /opt":                                {RC: 0, Stdout: "#0             1024        2048        2048          00 [--------]\n"},
		"xfs_quota -x -c 'report -p -i' /opt":                                {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'report -p -r' /opt":                                {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'limit -p -d bsoft=0 bhard=0' /opt":                 {RC: 0},
	})
	res, err := moduleXfsQuota(context.Background(), conn, map[string]any{
		"type": "project", "mountpoint": "/opt", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleXfsQuotaUserInodeLimits(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awk -v mp=/home '$2 == mp && $3 == \"xfs\" {print $4}' /proc/mounts": {RC: 0, Stdout: "rw,uquota\n"},
		"xfs_quota -x -c 'report -u -b' /home":                                {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'report -u -i' /home":                                {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'report -u -r' /home":                                {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'limit -u -d isoft=1024 ihard=2048' /home":           {RC: 0},
	})
	res, err := moduleXfsQuota(context.Background(), conn, map[string]any{
		"type": "user", "mountpoint": "/home", "isoft": 1024, "ihard": 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleXfsQuotaNoLimitsNoChange(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awk -v mp=/home '$2 == mp && $3 == \"xfs\" {print $4}' /proc/mounts": {RC: 0, Stdout: "rw,uquota\n"},
		"xfs_quota -x -c 'report -u -b' /home":                                {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'report -u -i' /home":                                {RC: 0, Stdout: ""},
		"xfs_quota -x -c 'report -u -r' /home":                                {RC: 0, Stdout: ""},
	})
	res, err := moduleXfsQuota(context.Background(), conn, map[string]any{
		"type": "user", "mountpoint": "/home",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when no limits requested")
	}
}

func TestModuleXfsQuotaNotXfsMount(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awk -v mp=/data '$2 == mp && $3 == \"xfs\" {print $4}' /proc/mounts": {RC: 0, Stdout: ""},
	})
	res, err := moduleXfsQuota(context.Background(), conn, map[string]any{
		"type": "user", "mountpoint": "/data",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-xfs mount")
	}
}

func TestModuleXfsQuotaWrongMountOption(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"awk -v mp=/home '$2 == mp && $3 == \"xfs\" {print $4}' /proc/mounts": {RC: 0, Stdout: "rw,noatime\n"},
	})
	res, err := moduleXfsQuota(context.Background(), conn, map[string]any{
		"type": "user", "mountpoint": "/home",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the quota mount option is missing")
	}
}

func TestModuleXfsQuotaInvalidType(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXfsQuota(context.Background(), conn, map[string]any{
		"type": "bogus", "mountpoint": "/home",
	}); err == nil {
		t.Fatal("want error for invalid type")
	}
}
