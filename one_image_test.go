package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const oneEmptyImagePool = `<IMAGE_POOL></IMAGE_POOL>`

func oneImageXML(id, state, runningVMs int, name string) string {
	return `<IMAGE><ID>` + fmtAny(id) + `</ID><NAME>` + name + `</NAME><STATE>` + fmtAny(state) +
		`</STATE><RUNNING_VMS>` + fmtAny(runningVMs) + `</RUNNING_VMS><UNAME>oneadmin</UNAME><UID>0</UID>` +
		`<GNAME>oneadmin</GNAME><GID>0</GID><PERSISTENT>0</PERSISTENT><SIZE>10000</SIZE></IMAGE>`
}

func oneImagePoolXML(images ...string) string {
	out := "<IMAGE_POOL>"
	for _, i := range images {
		out += i
	}
	return out + "</IMAGE_POOL>"
}

func TestModuleOneImageFetchByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneimage": {RC: 0},
		"oneimage show 45 -x": {RC: 0, Stdout: oneImageXML(45, oneImageStateReady, 0, "app1")},
	})
	res, err := moduleOneImage(context.Background(), conn, map[string]any{"id": 45})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["name"] != "app1" {
		t.Fatalf("name = %v", res.Extra["name"])
	}
	if res.Extra["state"] != "READY" {
		t.Fatalf("state = %v", res.Extra["state"])
	}
}

func TestModuleOneImageNotFoundByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneimage": {RC: 0},
		"oneimage show 45 -x": {RC: 1},
	})
	res, err := moduleOneImage(context.Background(), conn, map[string]any{"id": 45})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: no image with that id")
	}
}

func TestModuleOneImageCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v oneimage": {{RC: 0}},
		"oneimage list -x": {
			{RC: 0, Stdout: oneEmptyImagePool},
			{RC: 0, Stdout: oneImagePoolXML(oneImageXML(1, oneImageStateReady, 0, "myyy-image"))},
		},
	})
	res, err := moduleOneImage(context.Background(), conn, map[string]any{
		"name": "myyy-image", "state": "present", "create": true,
		"datastore_id": 100, "template": "PATH = \"/x\"\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "oneimage create -d 100 -" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleOneImageEnable(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v oneimage": {{RC: 0}},
		"oneimage list -x": {
			{RC: 0, Stdout: oneImagePoolXML(oneImageXML(37, oneImageStateDisabled, 0, "bar-image"))},
		},
		"oneimage enable 37": {{RC: 0}},
		"oneimage show 37 -x": {
			{RC: 0, Stdout: oneImageXML(37, oneImageStateReady, 0, "bar-image")},
		},
	})
	res, err := moduleOneImage(context.Background(), conn, map[string]any{
		"name": "bar-image", "enabled": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneImageDeleteInUse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneimage": {RC: 0},
		"oneimage show 37 -x": {RC: 0, Stdout: oneImageXML(37, oneImageStateReady, 3, "app1")},
	})
	res, err := moduleOneImage(context.Background(), conn, map[string]any{
		"id": 37, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: image in use")
	}
}

func TestModuleOneImageDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneimage": {RC: 0},
		"oneimage show 37 -x": {RC: 0, Stdout: oneImageXML(37, oneImageStateReady, 0, "app1")},
		"oneimage delete 37":  {RC: 0},
	})
	res, err := moduleOneImage(context.Background(), conn, map[string]any{
		"id": 37, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneImageRename(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"command -v oneimage": {{RC: 0}},
		"oneimage show 34 -x": {
			{RC: 0, Stdout: oneImageXML(34, oneImageStateReady, 0, "foo-image")},
		},
		"oneimage list -x":             {{RC: 0, Stdout: oneEmptyImagePool}},
		"oneimage rename 34 bar-image": {{RC: 0}},
	})
	res, err := moduleOneImage(context.Background(), conn, map[string]any{
		"id": 34, "state": "renamed", "new_name": "bar-image",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleOneImageValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleOneImage(context.Background(), conn, map[string]any{"id": 1, "name": "x"}); err == nil {
		t.Fatal("want error: id/name mutually exclusive")
	}
	if _, err := moduleOneImage(context.Background(), conn, map[string]any{"state": "renamed"}); err == nil {
		t.Fatal("want error: id required for renamed")
	}
	if _, err := moduleOneImage(context.Background(), conn, map[string]any{"name": "x", "create": true}); err == nil {
		t.Fatal("want error: template/datastore_id required for create")
	}
}
