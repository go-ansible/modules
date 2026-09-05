package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIloRedfishCommandWaitAlreadyDone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ilorest": {RC: 0},
		"ilorest rawget /redfish/v1/Systems/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/"}]}`,
		},
		"ilorest rawget /redfish/v1/Systems/1/": {
			RC: 0, Stdout: `{"Oem":{"Hpe":{"PostState":"FinishedPost"}}}`,
		},
	})
	args := map[string]any{"category": "Systems", "command": []any{"WaitforiLORebootCompletion"}, "baseuri": "x"}
	res, err := moduleIloRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIloRedfishCommandPoweredOff(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ilorest": {RC: 0},
		"ilorest rawget /redfish/v1/Systems/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/"}]}`,
		},
		"ilorest rawget /redfish/v1/Systems/1/": {
			RC: 0, Stdout: `{"Oem":{"Hpe":{"PostState":"Off"}}}`,
		},
	})
	args := map[string]any{"category": "Systems", "command": []any{"WaitforiLORebootCompletion"}, "baseuri": "x"}
	res, err := moduleIloRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
