package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSssdInfoDomainList(t *testing.T) {
	callCmd := "busctl --system --json=short call org.freedesktop.sssd.infopipe " +
		"/org/freedesktop/sssd/infopipe org.freedesktop.sssd.infopipe ListDomains"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v busctl": {RC: 0},
		callCmd: {RC: 0, Stdout: `{"type":"ao","data":[["/org/freedesktop/sssd/infopipe/Domains/ipa_2edomain",` +
			`"/org/freedesktop/sssd/infopipe/Domains/winad_2etest"]]}`},
	})
	res, err := moduleSssdInfo(context.Background(), fc, map[string]any{"action": "domain_list"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	list, ok := res.Extra["domain_list"].([]string)
	if !ok || len(list) != 2 || list[0] != "ipa.domain" || list[1] != "winad.test" {
		t.Fatalf("domain_list = %#v", res.Extra["domain_list"])
	}
}

func TestModuleSssdInfoDomainStatus(t *testing.T) {
	obj := "/org/freedesktop/sssd/infopipe/Domains/example_2ecom"
	callCmd := "busctl --system --json=short call org.freedesktop.sssd.infopipe " + obj +
		" org.freedesktop.sssd.infopipe.Domains.Domain IsOnline"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v busctl": {RC: 0},
		callCmd:             {RC: 0, Stdout: `{"type":"b","data":[true]}`},
	})
	res, err := moduleSssdInfo(context.Background(), fc, map[string]any{
		"action": "domain_status", "domain": "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["online"] != "online" {
		t.Fatalf("online = %v", res.Extra["online"])
	}
}

func TestModuleSssdInfoActiveServersIPA(t *testing.T) {
	obj := "/org/freedesktop/sssd/infopipe/Domains/example_2ecom"
	callCmd := "busctl --system --json=short call org.freedesktop.sssd.infopipe " + obj +
		" org.freedesktop.sssd.infopipe.Domains.Domain ActiveServer s IPA"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v busctl": {RC: 0},
		callCmd:             {RC: 0, Stdout: `{"type":"s","data":["ipaserver.example.com"]}`},
	})
	res, err := moduleSssdInfo(context.Background(), fc, map[string]any{
		"action": "active_servers", "domain": "example.com", "server_type": "IPA",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	servers, ok := res.Extra["servers"].(map[string]any)
	if !ok || servers["IPA Server"] != "ipaserver.example.com" {
		t.Fatalf("servers = %#v", res.Extra["servers"])
	}
}

func TestModuleSssdInfoListServersAD(t *testing.T) {
	obj := "/org/freedesktop/sssd/infopipe/Domains/winad_2etest"
	callCmd := "busctl --system --json=short call org.freedesktop.sssd.infopipe " + obj +
		" org.freedesktop.sssd.infopipe.Domains.Domain ListServers s sd_winad.test"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v busctl": {RC: 0},
		callCmd:             {RC: 0, Stdout: `{"type":"as","data":[["server1.winad.test","server2.winad.test"]]}`},
	})
	res, err := moduleSssdInfo(context.Background(), fc, map[string]any{
		"action": "list_servers", "domain": "winad.test", "server_type": "AD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	list, ok := res.Extra["list_servers"].([]string)
	if !ok || len(list) != 2 {
		t.Fatalf("list_servers = %#v", res.Extra["list_servers"])
	}
}

func TestModuleSssdInfoMissingDomain(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleSssdInfo(context.Background(), fc, map[string]any{"action": "domain_status"}); err == nil {
		t.Fatal("want error: domain required for domain_status")
	}
}

func TestModuleSssdInfoInvalidServerType(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleSssdInfo(context.Background(), fc, map[string]any{
		"action": "active_servers", "domain": "example.com", "server_type": "bogus",
	}); err == nil {
		t.Fatal("want error: server_type must be IPA or AD")
	}
}

func TestModuleSssdInfoNoBinary(t *testing.T) {
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v busctl": {RC: 1},
	})
	res, err := moduleSssdInfo(context.Background(), fc, map[string]any{"action": "domain_list"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
