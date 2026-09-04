package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const oneEmptyTemplatePool = `<VMTEMPLATE_POOL></VMTEMPLATE_POOL>`

func oneTemplateXML(id int, name, template string) string {
	return `<VMTEMPLATE><ID>` + fmtAny(id) + `</ID><NAME>` + name + `</NAME><UNAME>oneadmin</UNAME><UID>0</UID>` +
		`<GNAME>oneadmin</GNAME><GID>0</GID><TEMPLATE>` + template + `</TEMPLATE></VMTEMPLATE>`
}

func oneTemplatePoolXML(templates ...string) string {
	out := "<VMTEMPLATE_POOL>"
	for _, tt := range templates {
		out += tt
	}
	return out + "</VMTEMPLATE_POOL>"
}

func TestModuleOneTemplateFetchByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onetemplate":   {RC: 0},
		"onetemplate show 6459 -x": {RC: 0, Stdout: oneTemplateXML(6459, "tf-prd", "<CPU>1</CPU>")},
	})
	res, err := moduleOneTemplate(context.Background(), conn, map[string]any{
		"id": 6459, "template": "CPU = \"1\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["name"] != "tf-prd" {
		t.Fatalf("name = %v", res.Extra["name"])
	}
}

func TestModuleOneTemplateCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v onetemplate": {{RC: 0}},
		"onetemplate list -x": {
			{RC: 0, Stdout: oneEmptyTemplatePool},
			{RC: 0, Stdout: oneTemplatePoolXML(oneTemplateXML(1, "generic-opensuse", "<CPU>1</CPU>"))},
		},
	})
	res, err := moduleOneTemplate(context.Background(), conn, map[string]any{
		"name": "generic-opensuse", "template": "CPU = \"1\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "onetemplate create -" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleOneTemplateUpdateChanged(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v onetemplate":    {{RC: 0}},
		"onetemplate list -x":       {{RC: 0, Stdout: oneTemplatePoolXML(oneTemplateXML(6459, "tf-prd", "<CPU>1</CPU>"))}},
		"onetemplate update 6459 -": {{RC: 0}},
		"onetemplate show 6459 -x":  {{RC: 0, Stdout: oneTemplateXML(6459, "tf-prd", "<CPU>2</CPU>")}},
	})
	res, err := moduleOneTemplate(context.Background(), conn, map[string]any{
		"name": "tf-prd", "template": "CPU = \"2\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: CPU differs")
	}
}

func TestModuleOneTemplateUpdateUnchanged(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v onetemplate":    {{RC: 0}},
		"onetemplate list -x":       {{RC: 0, Stdout: oneTemplatePoolXML(oneTemplateXML(6459, "tf-prd", "<CPU>1</CPU>"))}},
		"onetemplate update 6459 -": {{RC: 0}},
		"onetemplate show 6459 -x":  {{RC: 0, Stdout: oneTemplateXML(6459, "tf-prd", "<CPU>1</CPU>")}},
	})
	res, err := moduleOneTemplate(context.Background(), conn, map[string]any{
		"name": "tf-prd", "template": "CPU = \"1\"",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: content identical, still updates but reports unchanged")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "onetemplate update 6459 -" {
			found = true
		}
	}
	if !found {
		t.Fatal("real one_template always issues the update call, even when unchanged")
	}
}

func TestModuleOneTemplateDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v onetemplate":   {RC: 0},
		"onetemplate show 6459 -x": {RC: 0, Stdout: oneTemplateXML(6459, "tf-prd", "")},
		"onetemplate delete 6459":  {RC: 0},
	})
	res, err := moduleOneTemplate(context.Background(), conn, map[string]any{
		"id": 6459, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneTemplateValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOneTemplate(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: id or name required")
	}
	if _, err := moduleOneTemplate(context.Background(), conn, map[string]any{"id": 1, "name": "x"}); err == nil {
		t.Fatal("want error: id/name mutually exclusive")
	}
	if _, err := moduleOneTemplate(context.Background(), conn, map[string]any{"id": 1}); err == nil {
		t.Fatal("want error: template required for present")
	}
}
