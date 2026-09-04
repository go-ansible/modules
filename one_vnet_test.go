package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const oneEmptyVnetPool = `<VNET_POOL></VNET_POOL>`

func oneVnetXML(id int, name, template string) string {
	return `<VNET><ID>` + fmtAny(id) + `</ID><NAME>` + name + `</NAME><UNAME>oneadmin</UNAME><UID>0</UID>` +
		`<GNAME>oneadmin</GNAME><GID>0</GID><BRIDGE>br0</BRIDGE><BRIDGE_TYPE>linux</BRIDGE_TYPE>` +
		`<TEMPLATE>` + template + `</TEMPLATE></VNET>`
}

func oneVnetPoolXML(vnets ...string) string {
	out := "<VNET_POOL>"
	for _, v := range vnets {
		out += v
	}
	return out + "</VNET_POOL>"
}

func TestModuleOneVnetFetchByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onevnet": {RC: 0},
		"onevnet show 12 -x": {RC: 0, Stdout: oneVnetXML(12, "opennebula-bridge", "")},
	})
	res, err := moduleOneVnet(context.Background(), conn, map[string]any{
		"id": 12, "template": "VN_MAD = \"bridge\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["bride_type"] != "linux" {
		t.Fatalf("bride_type = %v", res.Extra["bride_type"])
	}
}

func TestModuleOneVnetCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v onevnet": {{RC: 0}},
		"onevnet list -x": {
			{RC: 0, Stdout: oneEmptyVnetPool},
			{RC: 0, Stdout: oneVnetPoolXML(oneVnetXML(1, "bridge-network", "<VN_MAD>bridge</VN_MAD>"))},
		},
	})
	res, err := moduleOneVnet(context.Background(), conn, map[string]any{
		"name": "bridge-network", "template": "VN_MAD = \"bridge\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "onevnet create -" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleOneVnetDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onevnet": {RC: 0},
		"onevnet show 12 -x": {RC: 0, Stdout: oneVnetXML(12, "opennebula-bridge", "")},
		"onevnet delete 12":  {RC: 0},
	})
	res, err := moduleOneVnet(context.Background(), conn, map[string]any{
		"id": 12, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneVnetValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOneVnet(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: id or name required")
	}
	if _, err := moduleOneVnet(context.Background(), conn, map[string]any{"id": 1, "name": "x"}); err == nil {
		t.Fatal("want error: id/name mutually exclusive")
	}
	if _, err := moduleOneVnet(context.Background(), conn, map[string]any{"id": 1}); err == nil {
		t.Fatal("want error: template required for present")
	}
}
