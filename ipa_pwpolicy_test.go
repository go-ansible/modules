package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaPwpolicyCreateForGroup(t *testing.T) {
	showCmd := "ipa pwpolicy-show admins --all --raw"
	addCmd := "ipa pwpolicy-add admins --maxlife=60 --minlife=24 --history=16 --minclasses=4 --minlength=6 " +
		"--priority=10 --maxfail=4 --failinterval=600 --lockouttime=1200 --gracelimit=3 --maxrepeat=3 " +
		"--maxsequence=3 --dictcheck=TRUE --usercheck=TRUE"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaPwpolicy(context.Background(), fc, map[string]any{
		"group": "admins", "state": "present",
		"maxpwdlife": "60", "minpwdlife": "24", "historylength": "16", "minclasses": "4",
		"priority": "10", "minlength": "6", "maxfailcount": "4", "failinterval": "600",
		"lockouttime": "1200", "gracelimit": 3, "maxrepeat": 3, "maxsequence": 3,
		"dictcheck": true, "usercheck": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaPwpolicyGlobalPolicyDefaultGroup(t *testing.T) {
	showCmd := "ipa pwpolicy-show global_policy --all --raw"
	modCmd := "ipa pwpolicy-mod global_policy --maxlife=90"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: global_policy\n  krbmaxpwdlife: 60\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaPwpolicy(context.Background(), fc, map[string]any{
		"maxpwdlife": "90",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaPwpolicyAbsent(t *testing.T) {
	showCmd := "ipa pwpolicy-show sysops --all --raw"
	delCmd := "ipa pwpolicy-del sysops"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: sysops\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaPwpolicy(context.Background(), fc, map[string]any{
		"group": "sysops", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaPwpolicyNoop(t *testing.T) {
	showCmd := "ipa pwpolicy-show global_policy --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  krbmaxpwdlife: 90\n"},
	})
	res, err := moduleIpaPwpolicy(context.Background(), fc, map[string]any{
		"maxpwdlife": "90",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
