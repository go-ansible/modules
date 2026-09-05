package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIloRedfishInfoGetSessions(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ilorest": {RC: 0},
		"ilorest rawget /redfish/v1/SessionService/Sessions/": {
			RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/SessionService/Sessions/1/"}]}`,
		},
		"ilorest rawget /redfish/v1/SessionService/Sessions/1/": {
			RC: 0, Stdout: `{"Id":"1","Name":"Session","UserName":"admin","Description":"iLO session"}`,
		},
	})
	args := map[string]any{"category": []any{"Sessions"}, "command": []any{"GetiLOSessions"}, "baseuri": "x"}
	res, err := moduleIloRedfishInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	info, _ := res.Extra["ilo_redfish_info"].(map[string]any)
	get, _ := info["GetiLOSessions"].(map[string]any)
	sessions, _ := get["msg"].([]map[string]any)
	if len(sessions) != 1 || sessions[0]["UserName"] != "admin" {
		t.Fatalf("sessions = %+v", sessions)
	}
}

func TestModuleIloRedfishInfoInvalidCategory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ilorest": {RC: 0},
	})
	args := map[string]any{"category": []any{"Bogus"}, "command": []any{"GetiLOSessions"}, "baseuri": "x"}
	res, err := moduleIloRedfishInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
