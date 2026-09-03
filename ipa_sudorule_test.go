package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaSudoruleCreateAllCategories(t *testing.T) {
	showCmd := "ipa sudorule-show sudo_all_nopasswd --all --raw"
	addCmd := "ipa sudorule-add sudo_all_nopasswd --description=Allow to run every command with sudo without password --hostcat=all --usercat=all --cmdcat=all"
	addOptCmd := "ipa sudorule-add-option sudo_all_nopasswd '--ipasudoopt=!authenticate'"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
		addOptCmd:        {RC: 0},
	})
	res, err := moduleIpaSudorule(context.Background(), fc, map[string]any{
		"name": "sudo_all_nopasswd", "cmdcategory": "all", "hostcategory": "all", "usercategory": "all",
		"description": "Allow to run every command with sudo without password",
		"sudoopt":     []any{"!authenticate"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSudoruleCategoryConflict(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaSudorule(context.Background(), fc, map[string]any{
		"name": "x", "hostcategory": "all", "host": []any{"h1"},
	}); err == nil {
		t.Fatal("want error: hostcategory=all conflicts with an explicit host list")
	}
}

func TestModuleIpaSudoruleHostsAndUsergroup(t *testing.T) {
	showCmd := "ipa sudorule-show sudo_dev_dbserver --all --raw"
	addHostCmd := "ipa sudorule-add-host sudo_dev_dbserver --host=db01.example.com"
	addHostgroupCmd := "ipa sudorule-add-host sudo_dev_dbserver --hostgroup=db-server"
	addUsergroupCmd := "ipa sudorule-add-user sudo_dev_dbserver --group=developers"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: sudo_dev_dbserver\n"},
		addHostCmd:       {RC: 0},
		addHostgroupCmd:  {RC: 0},
		addUsergroupCmd:  {RC: 0},
	})
	res, err := moduleIpaSudorule(context.Background(), fc, map[string]any{
		"name": "sudo_dev_dbserver", "host": []any{"db01.example.com"}, "hostgroup": []any{"db-server"},
		"usergroup": []any{"developers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSudoruleAllowAndDenyCommands(t *testing.T) {
	showCmd := "ipa sudorule-show r --all --raw"
	allowCmd := "ipa sudorule-add-allow-command r --sudocmd=/bin/ls"
	denyCmd := "ipa sudorule-add-deny-command r --sudocmd=/bin/rm"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: r\n"},
		allowCmd:         {RC: 0},
		denyCmd:          {RC: 0},
	})
	res, err := moduleIpaSudorule(context.Background(), fc, map[string]any{
		"name": "r", "cmd": []any{"/bin/ls"}, "deny_cmd": []any{"/bin/rm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSudoruleDisable(t *testing.T) {
	showCmd := "ipa sudorule-show r --all --raw"
	modCmd := "ipa sudorule-mod r --ipaenabledflag=FALSE"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: r\n  ipaenabledflag: TRUE\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaSudorule(context.Background(), fc, map[string]any{"name": "r", "state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSudoruleDelete(t *testing.T) {
	showCmd := "ipa sudorule-show r --all --raw"
	delCmd := "ipa sudorule-del r"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: r\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaSudorule(context.Background(), fc, map[string]any{"name": "r", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaSudoruleMissingCN(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaSudorule(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing cn")
	}
}
