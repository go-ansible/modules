package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func vmParamListStdout() string {
	return "" +
		"    uuid ( RO): vm-uuid-1\n" +
		"    name-label ( RW): myvm\n" +
		"    name-description ( RW): a test VM\n" +
		"    power-state ( RO): running\n" +
		"    is-a-template ( RW): false\n" +
		"    dom-id ( RO): 12\n" +
		"    affinity ( RW): \n" +
		"    VCPUs-max ( RW): 2\n" +
		"    platform (MRW): cores-per-socket: 1\n" +
		"    memory-dynamic-max ( RW): 2147483648\n" +
		"    other-config (MRW): \n" +
		"    xenstore-data (MRW): \n" +
		"    VBDs ( RO): \n" +
		"    VIFs ( RO): \n"
}

func TestModuleXenserverGuestInfoMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXenserverGuestInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name/uuid")
	}
}

func TestModuleXenserverGuestInfoNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list name-label=myvm params=uuid --minimal": {RC: 0, Stdout: ""},
	})
	res, err := moduleXenserverGuestInfo(context.Background(), conn, map[string]any{"name": "myvm"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when VM not found")
	}
}

func TestModuleXenserverGuestInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list name-label=myvm params=uuid --minimal": {RC: 0, Stdout: "vm-uuid-1"},
		"xe vm-param-list uuid=vm-uuid-1":                   {RC: 0, Stdout: vmParamListStdout()},
	})
	res, err := moduleXenserverGuestInfo(context.Background(), conn, map[string]any{"name": "myvm"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	instance, ok := res.Extra["instance"].(map[string]any)
	if !ok {
		t.Fatalf("instance = %v", res.Extra["instance"])
	}
	if instance["name"] != "myvm" || instance["state"] != "poweredon" {
		t.Fatalf("instance = %+v", instance)
	}
	hw, ok := instance["hardware"].(map[string]any)
	if !ok {
		t.Fatalf("hardware = %v", instance["hardware"])
	}
	if hw["num_cpus"] != 2 || hw["memory_mb"] != 2048 {
		t.Fatalf("hardware = %+v", hw)
	}
}
