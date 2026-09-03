package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleNsupdateCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dig >/dev/null 2>&1":                                 {RC: 0},
		"dig +noall +answer +tcp @10.1.1.1 -p 53 ansible.example.org. A": {RC: 0, Stdout: ""},
		"nsupdate -y hmac-md5:nsupdate:secret -v -t 10":                  {RC: 0},
	})
	res, err := moduleNsupdate(context.Background(), conn, map[string]any{
		"key_name": "nsupdate", "key_secret": "secret", "server": "10.1.1.1",
		"zone": "example.org", "record": "ansible", "value": []any{"192.168.1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	// verify the composed nsupdate script content
	if len(conn.Stdins) == 0 {
		t.Fatal("expected an nsupdate call carrying a stdin script")
	}
	script := conn.Stdins[len(conn.Stdins)-1]
	if !strings.Contains(script, "server 10.1.1.1\n") ||
		!strings.Contains(script, "zone example.org.\n") ||
		!strings.Contains(script, "update delete ansible A\n") ||
		!strings.Contains(script, "update add ansible 3600 A 192.168.1.1\n") ||
		!strings.Contains(script, "send\n") {
		t.Fatalf("script = %q", script)
	}
}

func TestModuleNsupdateIdempotentWhenAlreadyMatching(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dig >/dev/null 2>&1": {RC: 0},
		"dig +noall +answer +tcp @10.1.1.1 -p 53 ansible.example.org. A": {
			RC: 0, Stdout: "ansible.example.org. 3600 IN A 192.168.1.1\n",
		},
	})
	res, err := moduleNsupdate(context.Background(), conn, map[string]any{
		"server": "10.1.1.1", "zone": "example.org", "record": "ansible", "value": []any{"192.168.1.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged", res)
	}
	for _, c := range conn.Commands {
		if strings.HasPrefix(c, "nsupdate") {
			t.Fatalf("should not have run nsupdate when already matching: %v", conn.Commands)
		}
	}
}

func TestModuleNsupdateRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dig >/dev/null 2>&1": {RC: 0},
		"dig +noall +answer +tcp @10.1.1.1 -p 53 puppet.example.org. CNAME": {
			RC: 0, Stdout: "puppet.example.org. 3600 IN CNAME target.example.org.\n",
		},
		"nsupdate -v -t 10": {RC: 0},
	})
	res, err := moduleNsupdate(context.Background(), conn, map[string]any{
		"server": "10.1.1.1", "zone": "example.org", "record": "puppet",
		"type": "CNAME", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	script := conn.Stdins[len(conn.Stdins)-1]
	if strings.Contains(script, "update add") {
		t.Fatalf("script should not add anything for state=absent: %q", script)
	}
}

func TestModuleNsupdateAbsentAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v dig >/dev/null 2>&1":                                    {RC: 0},
		"dig +noall +answer +tcp @10.1.1.1 -p 53 puppet.example.org. CNAME": {RC: 0, Stdout: ""},
	})
	res, err := moduleNsupdate(context.Background(), conn, map[string]any{
		"server": "10.1.1.1", "zone": "example.org", "record": "puppet",
		"type": "CNAME", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: nothing to remove")
	}
}

func TestModuleNsupdateAbsoluteRecordRequiredWithoutZone(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleNsupdate(context.Background(), conn, map[string]any{
		"server": "10.1.1.1", "record": "ansible", "value": []any{"1.2.3.4"},
	})
	if err == nil {
		t.Fatal("want error when zone is omitted and record is not absolute")
	}
}

func TestModuleNsupdateGssTsigRejectsKeyName(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleNsupdate(context.Background(), conn, map[string]any{
		"server": "10.1.1.1", "zone": "example.org", "record": "ansible",
		"key_algorithm": "gss-tsig", "key_name": "x", "value": []any{"1.2.3.4"},
	})
	if err == nil {
		t.Fatal("want error when key_name given with gss-tsig")
	}
}

func TestModuleNsupdateMissingValueForPresent(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleNsupdate(context.Background(), conn, map[string]any{
		"server": "10.1.1.1", "zone": "example.org", "record": "ansible",
	})
	if err == nil {
		t.Fatal("want error when value missing for state=present")
	}
}

func TestNsupdateTxtWrap(t *testing.T) {
	got := nsupdateTxtWrap("TXT", []string{"hello", `"already quoted"`})
	if got[0] != `"hello"` || got[1] != `"already quoted"` {
		t.Fatalf("got = %v", got)
	}
	got2 := nsupdateTxtWrap("A", []string{"1.2.3.4"})
	if got2[0] != "1.2.3.4" {
		t.Fatalf("got2 = %v", got2)
	}
}
