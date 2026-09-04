package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXenserverGuestMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXenserverGuest(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name/uuid")
	}
}

func TestModuleXenserverGuestInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleXenserverGuest(context.Background(), conn, map[string]any{
		"name": "myvm", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}

func TestModuleXenserverGuestAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list name-label=myvm params=uuid --minimal": {RC: 0, Stdout: ""},
	})
	res, err := moduleXenserverGuest(context.Background(), conn, map[string]any{
		"name": "myvm", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("want a no-op ok, got %+v", res)
	}
}

func TestModuleXenserverGuestAbsentFailsOnRunningWithoutForce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list name-label=myvm params=uuid --minimal":      {RC: 0, Stdout: "vm-uuid-1"},
		"xe vm-param-get uuid=vm-uuid-1 param-name=power-state": {RC: 0, Stdout: "running"},
	})
	res, err := moduleXenserverGuest(context.Background(), conn, map[string]any{
		"name": "myvm", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed: running VM cannot be removed without force, got %+v", res)
	}
}

func TestModuleXenserverGuestAbsentRemovesShutOffVM(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list name-label=myvm params=uuid --minimal":      {RC: 0, Stdout: "vm-uuid-1"},
		"xe vm-param-get uuid=vm-uuid-1 param-name=power-state": {RC: 0, Stdout: "halted"},
		"xe vm-uninstall uuid=vm-uuid-1 --force":                {RC: 0},
	})
	res, err := moduleXenserverGuest(context.Background(), conn, map[string]any{
		"name": "myvm", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, got %+v", res)
	}
	changes, _ := res.Extra["changes"].([]string)
	if len(changes) != 1 || changes[0] != "destroy" {
		t.Fatalf("changes = %v", changes)
	}
}

func TestModuleXenserverGuestPresentCreatesFromTemplate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list name-label=myvm params=uuid --minimal":       {RC: 0, Stdout: ""},
		"xe vm-list name-label=mytemplate params=uuid --minimal": {RC: 0, Stdout: "tmpl-uuid-1"},
		"xe vm-copy uuid=tmpl-uuid-1 new-name-label=myvm":        {RC: 0, Stdout: "vm-uuid-2\n"},
		"xe vm-param-set uuid=vm-uuid-2 is-a-template=false":     {RC: 0},
		"xe vm-param-get uuid=vm-uuid-2 param-name=power-state":  {RC: 0, Stdout: "halted"},
		"xe vm-param-list uuid=vm-uuid-2":                        {RC: 0, Stdout: vmParamListStdout()},
	})
	res, err := moduleXenserverGuest(context.Background(), conn, map[string]any{
		"name": "myvm", "template": "mytemplate", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, got %+v", res)
	}
	changes, _ := res.Extra["changes"].([]string)
	if len(changes) != 1 || changes[0] != "create" {
		t.Fatalf("changes = %v", changes)
	}
}

func TestModuleXenserverGuestPresentNoTemplateFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list name-label=myvm params=uuid --minimal": {RC: 0, Stdout: ""},
	})
	res, err := moduleXenserverGuest(context.Background(), conn, map[string]any{
		"name": "myvm", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed: template or template_uuid required to create a new VM, got %+v", res)
	}
}

func TestModuleXenserverGuestPresentUpdatesNameDesc(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list uuid=vm-uuid-1 params=uuid --minimal":         {RC: 0, Stdout: "vm-uuid-1"},
		"xe vm-param-get uuid=vm-uuid-1 param-name=power-state":   {RC: 0, Stdout: "halted"},
		"xe vm-param-set uuid=vm-uuid-1 name-description=updated": {RC: 0},
		"xe vm-param-list uuid=vm-uuid-1":                         {RC: 0, Stdout: vmParamListStdout()},
	})
	res, err := moduleXenserverGuest(context.Background(), conn, map[string]any{
		"uuid": "vm-uuid-1", "name_desc": "updated", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, got %+v", res)
	}
	changes, _ := res.Extra["changes"].([]string)
	if len(changes) != 1 || changes[0] != "name_desc" {
		t.Fatalf("changes = %v", changes)
	}
}

func TestModuleXenserverGuestPoweredonTransition(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"xe vm-list uuid=vm-uuid-1 params=uuid --minimal":       {RC: 0, Stdout: "vm-uuid-1"},
		"xe vm-param-get uuid=vm-uuid-1 param-name=power-state": {RC: 0, Stdout: "halted"},
		"xe vm-start uuid=vm-uuid-1":                            {RC: 0},
		"xe vm-param-list uuid=vm-uuid-1":                       {RC: 0, Stdout: vmParamListStdout()},
	})
	res, err := moduleXenserverGuest(context.Background(), conn, map[string]any{
		"uuid": "vm-uuid-1", "state": "poweredon",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, got %+v", res)
	}
	changes, _ := res.Extra["changes"].([]string)
	found := false
	for _, c := range changes {
		if c == "poweredon" {
			found = true
		}
	}
	if !found {
		t.Fatalf("changes = %v, want to include poweredon", changes)
	}
}
