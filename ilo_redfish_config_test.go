package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIloRedfishConfigSetWINSReg(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ilorest": {RC: 0},
		"ilorest rawget /redfish/v1/Managers/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/"}]}`,
		},
		"ilorest rawget /redfish/v1/Managers/1/": {
			RC: 0, Stdout: `{"EthernetInterfaces":{"@odata.id":"/redfish/v1/Managers/1/EthernetInterfaces/"}}`,
		},
		"ilorest rawget /redfish/v1/Managers/1/EthernetInterfaces/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/EthernetInterfaces/1/"}]}`,
		},
	})
	args := map[string]any{
		"category": "Manager", "command": []any{"SetWINSReg"},
		"attribute_name": "WINSRegistration", "baseuri": "15.1.1.1", "username": "Admin", "password": "x",
	}
	res, err := moduleIloRedfishConfig(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	found := false
	for _, c := range conn.Commands {
		if strings.Contains(c, "ilorest rawpatch /tmp/ilorest-rawpatch.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a rawpatch call, commands = %v", conn.Commands)
	}
}

func TestModuleIloRedfishConfigInvalidCommand(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ilorest": {RC: 0},
	})
	args := map[string]any{
		"category": "Manager", "command": []any{"Bogus"}, "attribute_name": "x", "baseuri": "x",
	}
	res, err := moduleIloRedfishConfig(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
