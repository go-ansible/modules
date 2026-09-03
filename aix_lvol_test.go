package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const aixLsvgSample = "VOLUME GROUP:       testvg                   VG IDENTIFIER:  xxx\n" +
	"VG STATE:            active                   PP SIZE:        128 megabyte(s)\n" +
	"VG PERMISSION:       read/write               TOTAL PPs:      511 (65408 megabytes)\n" +
	"MAX LVs:             256                      FREE PPs:       255 (32640 megabytes)\n"

func TestModuleAixLvolCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg testvg": {RC: 0, Stdout: aixLsvgSample},
		"lslv testlv": {RC: 1},
		"mklv -t jfs2 -y testlv -c 1 -e x testvg 512M": {RC: 0},
	})
	res, err := moduleAixLvol(context.Background(), conn, map[string]any{
		"vg": "testvg", "lv": "testlv", "size": "512M",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixLvolCreateNoSize(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg testvg": {RC: 0, Stdout: aixLsvgSample},
		"lslv testlv": {RC: 1},
	})
	res, err := moduleAixLvol(context.Background(), conn, map[string]any{
		"vg": "testvg", "lv": "testlv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: size required to create")
	}
}

const aixLslvSample = "LOGICAL VOLUME:     testlv                   VOLUME GROUP:    testvg\n" +
	"LV IDENTIFIER:      xxx                      PERMISSION:      read/write\n" +
	"VG STATE:           active/complete          LV STATE:        opened/syncd\n" +
	"TYPE:               jfs2                     WRITE VERIFY:    off\n" +
	"MAX LPs:            512                      PP SIZE:         128 megabyte(s)\n" +
	"COPIES:             1                        SCHED POLICY:    parallel\n" +
	"LPs:                4                        PPs:             4\n" +
	"STALE PPs:          0                        BB POLICY:       relocatable\n" +
	"INTER-POLICY:       maximum                  RELOCATABLE:     yes\n"

func TestModuleAixLvolExtend(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg testvg":          {RC: 0, Stdout: aixLsvgSample},
		"lslv testlv":          {RC: 0, Stdout: aixLslvSample},
		"extendlv testlv 768M": {RC: 0},
	})
	res, err := moduleAixLvol(context.Background(), conn, map[string]any{
		"vg": "testvg", "lv": "testlv", "size": "1200M",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixLvolShrinkRefused(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg testvg": {RC: 0, Stdout: aixLsvgSample},
		"lslv testlv": {RC: 0, Stdout: aixLslvSample},
	})
	res, err := moduleAixLvol(context.Background(), conn, map[string]any{
		"vg": "testvg", "lv": "testlv", "size": "100M",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: shrinking is refused")
	}
}

func TestModuleAixLvolAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg testvg": {RC: 0, Stdout: aixLsvgSample},
		"lslv testlv": {RC: 0, Stdout: aixLslvSample},
	})
	res, err := moduleAixLvol(context.Background(), conn, map[string]any{
		"vg": "testvg", "lv": "testlv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixLvolRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg testvg":    {RC: 0, Stdout: aixLsvgSample},
		"lslv testlv":    {RC: 0, Stdout: aixLslvSample},
		"rmlv -f testlv": {RC: 0},
	})
	res, err := moduleAixLvol(context.Background(), conn, map[string]any{
		"vg": "testvg", "lv": "testlv", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAixLvolRemoveNotExist(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg testvg": {RC: 0, Stdout: aixLsvgSample},
		"lslv testlv": {RC: 1},
	})
	res, err := moduleAixLvol(context.Background(), conn, map[string]any{
		"vg": "testvg", "lv": "testlv", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleAixLvolVGMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lsvg testvg": {RC: 1},
	})
	res, err := moduleAixLvol(context.Background(), conn, map[string]any{
		"vg": "testvg", "lv": "testlv", "size": "512M",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: vg does not exist")
	}
}

func TestModuleAixLvolMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAixLvol(context.Background(), conn, map[string]any{"lv": "testlv"}); err == nil {
		t.Fatal("want error for missing vg")
	}
	if _, err := moduleAixLvol(context.Background(), conn, map[string]any{"vg": "testvg"}); err == nil {
		t.Fatal("want error for missing lv")
	}
}
