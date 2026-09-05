package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleXccRedfishCommandVirtualMediaInsert(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v OneCli": {RC: 0},
		"OneCli vm list":    {RC: 0, Stdout: "ID     Path   Status\nRDOC1  -      not mounted\n"},
		"OneCli vm mount --id RDOC1 --path http://example.com/images/SomeLinux-current.iso": {RC: 0},
	})
	args := map[string]any{
		"category": "Manager", "command": []any{"VirtualMediaInsert"}, "baseuri": "x",
		"virtual_media": map[string]any{"image_url": "http://example.com/images/SomeLinux-current.iso"},
		"resource_id":   "1",
	}
	res, err := moduleXccRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleXccRedfishCommandVirtualMediaEjectNoneInserted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v OneCli": {RC: 0},
		"OneCli vm list":    {RC: 0, Stdout: "ID     Path   Status\nRDOC1  -      not mounted\n"},
	})
	args := map[string]any{
		"category": "Manager", "command": []any{"VirtualMediaEject"}, "baseuri": "x",
	}
	res, err := moduleXccRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleXccRedfishCommandRawUnsupported(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	args := map[string]any{
		"category": "Raw", "command": []any{"GetResource"}, "baseuri": "x",
		"resource_uri": "/redfish/v1/Systems/1",
	}
	res, err := moduleXccRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}

func TestModuleXccRedfishCommandInvalidCategory(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	args := map[string]any{"category": "Bogus", "command": []any{"X"}, "baseuri": "x"}
	res, err := moduleXccRedfishCommand(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
