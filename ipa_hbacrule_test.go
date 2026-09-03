package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaHbacruleCreateAllowAll(t *testing.T) {
	showCmd := "ipa hbacrule-show allow_all --all --raw"
	addCmd := "ipa hbacrule-add allow_all --description=Allow all users to access any host from any host --hostcat=all --usercat=all --servicecat=all"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaHbacrule(context.Background(), fc, map[string]any{
		"name": "allow_all", "description": "Allow all users to access any host from any host",
		"hostcategory": "all", "servicecategory": "all", "usercategory": "all",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaHbacruleHostgroupAndUsergroup(t *testing.T) {
	showCmd := "ipa hbacrule-show allow_all_developers_access_to_db --all --raw"
	addHostgroupCmd := "ipa hbacrule-add-host allow_all_developers_access_to_db --hostgroup=db-server"
	addUsergroupCmd := "ipa hbacrule-add-user allow_all_developers_access_to_db --group=developers"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: allow_all_developers_access_to_db\n"},
		addHostgroupCmd:  {RC: 0},
		addUsergroupCmd:  {RC: 0},
	})
	res, err := moduleIpaHbacrule(context.Background(), fc, map[string]any{
		"name": "allow_all_developers_access_to_db", "hostgroup": []any{"db-server"}, "usergroup": []any{"developers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaHbacruleServiceAddOnly(t *testing.T) {
	showCmd := "ipa hbacrule-show r --all --raw"
	addSvcCmd := "ipa hbacrule-add-service r --hbacsvc=sshd"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: r\n"},
		addSvcCmd:        {RC: 0},
	})
	res, err := moduleIpaHbacrule(context.Background(), fc, map[string]any{
		"name": "r", "service": []any{"sshd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaHbacruleReconcileHostsExactly(t *testing.T) {
	showCmd := "ipa hbacrule-show r --all --raw"
	removeCmd := "ipa hbacrule-remove-host r --host=old.example.com"
	addCmd := "ipa hbacrule-add-host r --host=new.example.com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd: {RC: 0, Stdout: "  cn: r\n" +
			"  memberhost: fqdn=old.example.com,cn=computers,cn=accounts,dc=example,dc=com\n"},
		removeCmd: {RC: 0},
		addCmd:    {RC: 0},
	})
	res, err := moduleIpaHbacrule(context.Background(), fc, map[string]any{
		"name": "r", "host": []any{"new.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaHbacruleDisable(t *testing.T) {
	showCmd := "ipa hbacrule-show r --all --raw"
	modCmd := "ipa hbacrule-mod r --ipaenabledflag=FALSE"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: r\n  ipaenabledflag: TRUE\n"},
		modCmd:           {RC: 0},
	})
	res, err := moduleIpaHbacrule(context.Background(), fc, map[string]any{"name": "r", "state": "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaHbacruleDelete(t *testing.T) {
	showCmd := "ipa hbacrule-show r --all --raw"
	delCmd := "ipa hbacrule-del r"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  cn: r\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaHbacrule(context.Background(), fc, map[string]any{"name": "r", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaHbacruleMissingCN(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaHbacrule(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing cn")
	}
}
