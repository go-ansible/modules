package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func oneflowDocPool(docs ...string) string {
	out := `{"DOCUMENT_POOL":{"DOCUMENT":[`
	for i, d := range docs {
		if i > 0 {
			out += ","
		}
		out += d
	}
	return out + `]}}`
}

func oneflowServiceDoc(id, name string, state int, uid, gid int) string {
	return `{"ID":"` + id + `","NAME":"` + name + `","UID":"` + fmtAny(uid) + `","GID":"` + fmtAny(gid) +
		`","UNAME":"ansible-test","GNAME":"one-users","PERMISSIONS":{"OWNER_U":"1","OWNER_M":"1","OWNER_A":"0",` +
		`"GROUP_U":"0","GROUP_M":"0","GROUP_A":"0","OTHER_U":"0","OTHER_M":"0","OTHER_A":"0"},` +
		`"TEMPLATE":{"BODY":{"state":` + fmtAny(state) + `,"roles":[{"name":"foo","cardinality":1,"state":2,"nodes":[{"deploy_id":123}]}],"log":[]}}}`
}

func oneflowTemplateDoc(id, name string) string {
	return `{"ID":"` + id + `","NAME":"` + name + `"}`
}

func TestModuleOneServiceInstantiate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v oneflow":          {{RC: 0}},
		"command -v oneflow-template": {{RC: 0}},
		"oneflow-template list --json": {
			{RC: 0, Stdout: oneflowDocPool(oneflowTemplateDoc("90", "app1_template"))},
		},
		"oneflow-template instantiate 90 -": {{RC: 0}},
		"oneflow list --json": {
			{RC: 0, Stdout: oneflowDocPool(oneflowServiceDoc("200", "app1", 2, 143, 1))},
		},
	})
	res, err := moduleOneService(context.Background(), conn, map[string]any{
		"template_id": 90, "service_name": "app1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["service_id"] != 200 {
		t.Fatalf("service_id = %v", res.Extra["service_id"])
	}
	if res.Extra["state"] != "RUNNING" {
		t.Fatalf("state = %v", res.Extra["state"])
	}
}

func TestModuleOneServiceDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneflow":          {RC: 0},
		"command -v oneflow-template": {RC: 0},
		"oneflow list --json": {RC: 0, Stdout: oneflowDocPool(
			oneflowServiceDoc("153", "app1", 2, 143, 1),
		)},
		"oneflow delete 153": {RC: 0},
	})
	res, err := moduleOneService(context.Background(), conn, map[string]any{
		"service_id": 153, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneServiceChangeOwnerGroupMode(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v oneflow":          {{RC: 0}},
		"command -v oneflow-template": {{RC: 0}},
		"oneflow list --json": {
			{RC: 0, Stdout: oneflowDocPool(oneflowServiceDoc("153", "app2", 2, 143, 1))},
			{RC: 0, Stdout: oneflowDocPool(oneflowServiceDoc("153", "app2", 2, 34, 113))},
		},
		"oneflow chown 153 34":  {{RC: 0}},
		"oneflow chgrp 153 113": {{RC: 0}},
	})
	res, err := moduleOneService(context.Background(), conn, map[string]any{
		"service_name": "app2", "owner_id": 34, "group_id": 113,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["owner_id"] != 34 || res.Extra["group_id"] != 113 {
		t.Fatalf("owner_id/group_id = %v/%v", res.Extra["owner_id"], res.Extra["group_id"])
	}
}

func TestModuleOneServiceScaleCardinality(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v oneflow":          {{RC: 0}},
		"command -v oneflow-template": {{RC: 0}},
		"oneflow list --json": {
			{RC: 0, Stdout: oneflowDocPool(oneflowServiceDoc("112", "foo1", 2, 143, 1))},
		},
		"oneflow scale 112 foo 7": {{RC: 0}},
	})
	res, err := moduleOneService(context.Background(), conn, map[string]any{
		"service_id": 112, "role": "foo", "cardinality": 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneServiceValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOneService(context.Background(), conn, map[string]any{
		"service_id": 1, "service_name": "x",
	}); err == nil {
		t.Fatal("want error: service_id/service_name mutually exclusive")
	}
	if _, err := moduleOneService(context.Background(), conn, map[string]any{
		"template_id": 1, "role": "foo", "cardinality": 1,
	}); err == nil {
		t.Fatal("want error: template_id and role/cardinality mutually exclusive")
	}
	if _, err := moduleOneService(context.Background(), conn, map[string]any{
		"unique": true,
	}); err == nil {
		t.Fatal("want error: unique requires service_name")
	}
	if _, err := moduleOneService(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: need service id/name or template id/name")
	}
}
