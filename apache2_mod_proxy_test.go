package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const apacheMainPageFixture = `<html><body>
Server Version: Apache/2.4.41 (Ubuntu)
<a href="/balancer-manager/">Balancer Manager</a>
<a href="/balancer-manager/?b=mybalancer&w=http://10.10.0.20:8080/ws&nonce=8925436c-79c6-4841-8936-e7d13b79239b">http://10.10.0.20:8080/ws</a>
</body></html>`

const apacheMemberPageFixture = `<html><body>
<table><tr><th>x</th></tr><tr><td>y</td></tr></table>
<table>
<tr><th>Worker URL</th><th>Route</th><th>Status</th></tr>
<tr><td>http://10.10.0.20:8080/ws</td><td>&nbsp;</td><td>Init Ok </td></tr>
</table>
</body></html>`

const apacheManagementURL = "http://10.0.0.2/balancer-manager/?b=mybalancer&w=http://10.10.0.20:8080/ws&nonce=8925436c-79c6-4841-8936-e7d13b79239b"

func TestModuleApache2ModProxyListMembers(t *testing.T) {
	mainURL := "http://10.0.0.2/balancer-manager/"
	fc := newFakeConn(map[string]remoteexec.Result{
		apacheGetCmd(mainURL, "", true):                              {RC: 0, Stdout: apacheMainPageFixture + "\nHTTPSTATUS:200"},
		apacheGetCmd(apacheManagementURL, apacheManagementURL, true): {RC: 0, Stdout: apacheMemberPageFixture + "\nHTTPSTATUS:200"},
	})
	res, err := moduleApache2ModProxy(context.Background(), fc, map[string]any{"balancer_vhost": "10.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	members, ok := res.Extra["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members = %#v", res.Extra["members"])
	}
	m := members[0].(map[string]any)
	if m["host"] != "10.10.0.20" || m["port"] != "8080" || m["path"] != "/ws" || m["protocol"] != "http" {
		t.Fatalf("member = %#v", m)
	}
	status := m["status"].(map[string]bool)
	if status["disabled"] || status["drained"] {
		t.Fatalf("status = %#v", status)
	}
}

func TestModuleApache2ModProxyGetOneMember(t *testing.T) {
	mainURL := "http://10.0.0.2/balancer-manager/"
	fc := newFakeConn(map[string]remoteexec.Result{
		apacheGetCmd(mainURL, "", true):                              {RC: 0, Stdout: apacheMainPageFixture + "\nHTTPSTATUS:200"},
		apacheGetCmd(apacheManagementURL, apacheManagementURL, true): {RC: 0, Stdout: apacheMemberPageFixture + "\nHTTPSTATUS:200"},
	})
	res, err := moduleApache2ModProxy(context.Background(), fc, map[string]any{
		"balancer_vhost": "10.0.0.2", "member_host": "10.10.0.20",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	m, ok := res.Extra["member"].(map[string]any)
	if !ok || m["host"] != "10.10.0.20" {
		t.Fatalf("member = %#v", res.Extra["member"])
	}
}

func TestModuleApache2ModProxySetDrained(t *testing.T) {
	mainURL := "http://10.0.0.2/balancer-manager/"
	wantBody := "b=mybalancer&w=http://10.10.0.20:8080/ws&nonce=8925436c-79c6-4841-8936-e7d13b79239b&w_status_D=0&w_status_N=1&w_status_H=0&w_status_I=0"
	postCmd := apachePostCmd(apacheManagementURL, wantBody, true)
	fc := newFakeConn(map[string]remoteexec.Result{
		apacheGetCmd(mainURL, "", true):                              {RC: 0, Stdout: apacheMainPageFixture + "\nHTTPSTATUS:200"},
		apacheGetCmd(apacheManagementURL, apacheManagementURL, true): {RC: 0, Stdout: apacheMemberPageFixture + "\nHTTPSTATUS:200"},
		postCmd: {RC: 0, Stdout: "\nHTTPSTATUS:200"},
	})
	res, err := moduleApache2ModProxy(context.Background(), fc, map[string]any{
		"balancer_vhost": "10.0.0.2", "member_host": "10.10.0.20", "state": []any{"drained"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want changed when setting a member to drained")
	}
	found := false
	for _, c := range fc.Commands {
		if c == postCmd {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want the status POST", fc.Commands)
	}
}

func TestModuleApache2ModProxyMemberNotFound(t *testing.T) {
	mainURL := "http://10.0.0.2/balancer-manager/"
	fc := newFakeConn(map[string]remoteexec.Result{
		apacheGetCmd(mainURL, "", true):                              {RC: 0, Stdout: apacheMainPageFixture + "\nHTTPSTATUS:200"},
		apacheGetCmd(apacheManagementURL, apacheManagementURL, true): {RC: 0, Stdout: apacheMemberPageFixture + "\nHTTPSTATUS:200"},
	})
	res, err := moduleApache2ModProxy(context.Background(), fc, map[string]any{
		"balancer_vhost": "10.0.0.2", "member_host": "192.168.1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when member_host is not a member of the balancer")
	}
}

func TestModuleApache2ModProxyBadApacheVersion(t *testing.T) {
	mainURL := "http://10.0.0.2/balancer-manager/"
	page := `<html><body>Server Version: Apache/2.2.31</body></html>`
	fc := newFakeConn(map[string]remoteexec.Result{
		apacheGetCmd(mainURL, "", true): {RC: 0, Stdout: page + "\nHTTPSTATUS:200"},
	})
	res, err := moduleApache2ModProxy(context.Background(), fc, map[string]any{"balancer_vhost": "10.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-2.4 Apache version")
	}
}

func TestModuleApache2ModProxyMutuallyExclusiveStates(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleApache2ModProxy(context.Background(), fc, map[string]any{
		"balancer_vhost": "10.0.0.2", "state": []any{"present", "drained"},
	}); err == nil {
		t.Fatal("want error: present/enabled cannot be combined with other states")
	}
}

func TestModuleApache2ModProxyMissingVhost(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleApache2ModProxy(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing balancer_vhost")
	}
}

func TestApacheModProxyWantStatus(t *testing.T) {
	got := apacheModProxyWantStatus([]string{"absent"})
	if !got["disabled"] {
		t.Fatalf("absent should map to disabled=true: %#v", got)
	}
	got = apacheModProxyWantStatus([]string{"present"})
	for k, v := range got {
		if v {
			t.Fatalf("present should leave every status false: %#v (key %s)", got, k)
		}
	}
}
