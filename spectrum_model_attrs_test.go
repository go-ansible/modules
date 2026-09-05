package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func spectrumModelAttrsBaseArgs(attrs []any) map[string]any {
	return map[string]any{
		"url":          "http://oneclick.url.com",
		"url_username": "oneclick_username",
		"url_password": "oneclick_password",
		"name":         "modelxyz01",
		"type":         "Host_Device",
		"attributes":   attrs,
	}
}

func TestModuleSpectrumModelAttrsEnforce(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 0},
		`cd "$SPECROOT/vnmsh" && ./seek attr=0x1006e val=modelxyz01`: {RC: 0, Stdout: "mh=0x1010e76"},
		`cd "$SPECROOT/vnmsh" && ./current mh=0x1010e76`:             {RC: 0},
		`cd "$SPECROOT/vnmsh" && ./update attr=0x1295d,val=false`:    {RC: 0},
	})
	res, err := moduleSpectrumModelAttrs(context.Background(), conn, spectrumModelAttrsBaseArgs([]any{
		map[string]any{"name": "isManaged", "value": "false"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("want changed, not failed; res = %+v", res)
	}
	changed, _ := res.Extra["changed_attrs"].(map[string]any)
	if changed["isManaged"] != "false" {
		t.Fatalf("unexpected changed_attrs: %+v", changed)
	}
}

func TestModuleSpectrumModelAttrsUnknownAttrPassesHexThrough(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 0},
		`cd "$SPECROOT/vnmsh" && ./seek attr=0x1006e val=modelxyz01`: {RC: 0, Stdout: "mh=0x1010e76"},
		`cd "$SPECROOT/vnmsh" && ./current mh=0x1010e76`:             {RC: 0},
		`cd "$SPECROOT/vnmsh" && ./update attr=0x999999,val=x`:       {RC: 0},
	})
	res, err := moduleSpectrumModelAttrs(context.Background(), conn, spectrumModelAttrsBaseArgs([]any{
		map[string]any{"name": "0x999999", "value": "x"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSpectrumModelAttrsModelNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 0},
		`cd "$SPECROOT/vnmsh" && ./seek attr=0x1006e val=modelxyz01`: {RC: 1},
	})
	res, err := moduleSpectrumModelAttrs(context.Background(), conn, spectrumModelAttrsBaseArgs([]any{
		map[string]any{"name": "Notes", "value": "hi"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleSpectrumModelAttrsMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		spectrumBinaryCheckCmd: {RC: 1},
	})
	res, err := moduleSpectrumModelAttrs(context.Background(), conn, spectrumModelAttrsBaseArgs([]any{
		map[string]any{"name": "Notes", "value": "hi"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleSpectrumModelAttrsMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleSpectrumModelAttrs(context.Background(), conn, map[string]any{"name": "x"})
	if err == nil {
		t.Fatal("want error for missing required args")
	}
}
