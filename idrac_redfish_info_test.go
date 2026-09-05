package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIdracRedfishInfoGetManagerAttributes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v racadm": {RC: 0},
		"racadm get -f /tmp/idrac-racadm-get.ini -t ini && cat /tmp/idrac-racadm-get.ini; rm -f /tmp/idrac-racadm-get.ini": {
			RC: 0, Stdout: "[NTPConfigGroup.1]\nNTPEnable=Enabled\nNTP1=10.0.0.1\n\n[LCAttributes.1]\nCollectSystemInventoryOnRestart=Disabled\n",
		},
	})
	args := map[string]any{
		"category": "Manager", "command": []any{"GetManagerAttributes"}, "baseuri": "x",
	}
	res, err := moduleIdracRedfishInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facts, _ := res.Extra["redfish_facts"].(map[string]any)
	entries, _ := facts["entries"].([]map[string]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	attrs, _ := entries[0]["Attributes"].(map[string]string)
	if attrs["NTPConfigGroup.1.NTPEnable"] != "Enabled" {
		t.Fatalf("attrs = %+v", attrs)
	}
	if attrs["LCAttributes.1.CollectSystemInventoryOnRestart"] != "Disabled" {
		t.Fatalf("attrs = %+v", attrs)
	}
}

func TestModuleIdracRedfishInfoInvalidCommand(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	args := map[string]any{"category": "Manager", "command": []any{"Bogus"}, "baseuri": "x"}
	res, err := moduleIdracRedfishInfo(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
