package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func hpiloFakeConn(power string, extra map[string]remoteexec.Result) *fakeConn {
	cmds := map[string]remoteexec.Result{
		"command -v ilorest":                    {RC: 0},
		"ilorest rawget /redfish/v1/Systems/":   {RC: 0, Stdout: `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/"}]}`},
		"ilorest rawget /redfish/v1/Systems/1/": {RC: 0, Stdout: `{"PowerState":"` + power + `"}`},
	}
	for k, v := range extra {
		cmds[k] = v
	}
	return newFakeConn(cmds)
}

func TestModuleHpiloBootSetsOneTimeBootAndPowersOn(t *testing.T) {
	conn := hpiloFakeConn("Off", map[string]remoteexec.Result{
		"ilorest bootorder --onetimeboot=Cd && ilorest commit": {RC: 0},
		"ilorest reboot PushPowerButton":                       {RC: 0},
	})
	args := map[string]any{"host": "ilo.example.com", "media": "cdrom"}
	res, err := moduleHpiloBoot(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["power"] != "OFF" {
		t.Fatalf("power = %v", res.Extra["power"])
	}
}

func TestModuleHpiloBootAlreadyPoweredOnFailsWithoutForce(t *testing.T) {
	conn := hpiloFakeConn("On", nil)
	args := map[string]any{"host": "ilo.example.com"}
	res, err := moduleHpiloBoot(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleHpiloBootIdempotentBootOnceWhenAlreadyOn(t *testing.T) {
	conn := hpiloFakeConn("On", nil)
	args := map[string]any{"host": "ilo.example.com", "idempotent_boot_once": true}
	res, err := moduleHpiloBoot(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want ok and unchanged", res)
	}
}

func TestModuleHpiloBootForceWhileOnFailsLoud(t *testing.T) {
	conn := hpiloFakeConn("On", nil)
	args := map[string]any{"host": "ilo.example.com", "force": true}
	res, err := moduleHpiloBoot(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no graceful-restart-while-on primitive)", res)
	}
}

func TestModuleHpiloBootFloppyMediaFailsLoud(t *testing.T) {
	conn := hpiloFakeConn("Off", nil)
	args := map[string]any{"host": "ilo.example.com", "media": "floppy"}
	res, err := moduleHpiloBoot(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (no floppy boot target)", res)
	}
}

func TestModuleHpiloBootImageFailsLoud(t *testing.T) {
	conn := hpiloFakeConn("Off", nil)
	args := map[string]any{"host": "ilo.example.com", "media": "cdrom", "image": "http://example.com/boot.iso"}
	res, err := moduleHpiloBoot(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (virtual media insert not implemented)", res)
	}
}

func TestModuleHpiloBootPoweroffState(t *testing.T) {
	conn := hpiloFakeConn("On", map[string]remoteexec.Result{
		"ilorest reboot PressAndHold": {RC: 0},
	})
	args := map[string]any{"host": "ilo.example.com", "state": "poweroff"}
	res, err := moduleHpiloBoot(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHpiloBootPoweroffAlreadyOff(t *testing.T) {
	conn := hpiloFakeConn("Off", nil)
	args := map[string]any{"host": "ilo.example.com", "state": "poweroff"}
	res, err := moduleHpiloBoot(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v, want ok and unchanged", res)
	}
}
