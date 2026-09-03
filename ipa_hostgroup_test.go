package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaHostgroupCreate(t *testing.T) {
	showCmd := "ipa hostgroup-show databases --all --raw"
	addCmd := "ipa hostgroup-add databases"
	addHostCmd := "ipa hostgroup-add-member databases --host=db.example.com"
	addGroupCmd := "ipa hostgroup-add-member databases --hostgroup=mysql-server --hostgroup=oracle-server"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
		addHostCmd:       {RC: 0},
		addGroupCmd:      {RC: 0},
	})
	res, err := moduleIpaHostgroup(context.Background(), fc, map[string]any{
		"cn": "databases", "host": []any{"db.example.com"}, "hostgroup": []any{"mysql-server", "oracle-server"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaHostgroupStateDisabledMeansAbsent(t *testing.T) {
	showCmd := "ipa hostgroup-show databases --all --raw"
	delCmd := "ipa hostgroup-del databases"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: databases\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaHostgroup(context.Background(), fc, map[string]any{"cn": "databases", "state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want removed (disabled == absent per real ipa_hostgroup docs)", res)
	}
}

func TestModuleIpaHostgroupAlreadyAbsent(t *testing.T) {
	showCmd := "ipa hostgroup-show databases --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
	})
	res, err := moduleIpaHostgroup(context.Background(), fc, map[string]any{"cn": "databases", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
}
