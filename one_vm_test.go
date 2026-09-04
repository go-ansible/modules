package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func oneVMXML(id int, name string, state, lcmState int) string {
	return `<VM><ID>` + fmtAny(id) + `</ID><NAME>` + name + `</NAME><UID>143</UID><UNAME>app-user</UNAME>` +
		`<GID>1</GID><GNAME>one-users</GNAME><STATE>` + fmtAny(state) + `</STATE><LCM_STATE>` + fmtAny(lcmState) +
		`</LCM_STATE><PERMISSIONS><OWNER_U>1</OWNER_U><OWNER_M>1</OWNER_M><OWNER_A>0</OWNER_A><GROUP_U>0</GROUP_U>` +
		`<GROUP_M>0</GROUP_M><GROUP_A>0</GROUP_A><OTHER_U>0</OTHER_U><OTHER_M>0</OTHER_M><OTHER_A>0</OTHER_A></PERMISSIONS>` +
		`<TEMPLATE><CPU>0.2</CPU><VCPU>2</VCPU><MEMORY>4096</MEMORY><TEMPLATE_ID>90</TEMPLATE_ID></TEMPLATE></VM>`
}

func oneVMPoolXML(vms ...string) string {
	out := "<VM_POOL>"
	for _, v := range vms {
		out += v
	}
	return out + "</VM_POOL>"
}

func TestModuleOneVMCreateSingle(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onevm":           {RC: 0},
		"command -v onetemplate":     {RC: 0},
		"onetemplate instantiate 90": {RC: 0, Stdout: "VM ID: 153\n"},
		"onevm show 153 -x":          {RC: 0, Stdout: oneVMXML(153, "foo", 3, 3)},
	})
	res, err := moduleOneVM(context.Background(), conn, map[string]any{"template_id": 90})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	instances, _ := res.Extra["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("instances = %v", res.Extra["instances"])
	}
	fact := instances[0].(map[string]any)
	if fact["vm_id"] != 153 {
		t.Fatalf("vm_id = %v", fact["vm_id"])
	}
}

func TestModuleOneVMCreateCountWithNamePattern(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onevm":                         {RC: 0},
		"command -v onetemplate":                   {RC: 0},
		"onetemplate instantiate 90 --name foo-00": {RC: 0, Stdout: "VM ID: 10\n"},
		"onetemplate instantiate 90 --name foo-01": {RC: 0, Stdout: "VM ID: 11\n"},
		"onevm show 10 -x":                         {RC: 0, Stdout: oneVMXML(10, "foo-00", 3, 3)},
		"onevm show 11 -x":                         {RC: 0, Stdout: oneVMXML(11, "foo-01", 3, 3)},
	})
	res, err := moduleOneVM(context.Background(), conn, map[string]any{
		"template_id": 90, "count": 2,
		"attributes": map[string]any{"name": "foo-##"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	ids, _ := res.Extra["instances_ids"].([]any)
	if len(ids) != 2 {
		t.Fatalf("instances_ids = %v", res.Extra["instances_ids"])
	}
}

func TestModuleOneVMUnimplementedGapFailsLoud(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleOneVM(context.Background(), conn, map[string]any{
		"template_id": 90, "exact_count": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: exact_count is not implemented")
	}
}

func TestModuleOneVMTerminateByInstanceIDs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onevm":  {RC: 0},
		"onevm terminate 5": {RC: 0},
	})
	res, err := moduleOneVM(context.Background(), conn, map[string]any{
		"state": "absent", "instance_ids": []any{5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneVMRebootByNameMatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onevm": {RC: 0},
		"onevm list -x":    {RC: 0, Stdout: oneVMPoolXML(oneVMXML(20, "fooapp-1", 3, 3))},
		"onevm reboot 20":  {RC: 0},
		"onevm show 20 -x": {RC: 0, Stdout: oneVMXML(20, "fooapp-1", 3, 3)},
	})
	res, err := moduleOneVM(context.Background(), conn, map[string]any{
		"state": "rebooted", "attributes": map[string]any{"name": "fooapp-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneVMValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOneVM(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}

func TestOneVMDerivedName(t *testing.T) {
	if got := oneVMDerivedName("foo-###", 3); got != "foo-003" {
		t.Fatalf("derived name = %q", got)
	}
	if got := oneVMDerivedName("foo", 0); got != "foo" {
		t.Fatalf("derived name = %q", got)
	}
	if got := oneVMDerivedName("", 0); got != "" {
		t.Fatalf("derived name = %q", got)
	}
}
