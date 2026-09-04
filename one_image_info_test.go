package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleOneImageInfoAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneimage": {RC: 0},
		"oneimage list -x": {RC: 0, Stdout: oneImagePoolXML(
			oneImageXML(1, oneImageStateReady, 0, "foo-image"),
			oneImageXML(2, oneImageStateReady, 0, "app-image-1"),
		)},
	})
	res, err := moduleOneImageInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	images, _ := res.Extra["images"].([]any)
	if len(images) != 2 {
		t.Fatalf("images = %v", res.Extra["images"])
	}
}

func TestModuleOneImageInfoByIDs(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneimage": {RC: 0},
		"oneimage list -x": {RC: 0, Stdout: oneImagePoolXML(
			oneImageXML(1, oneImageStateReady, 0, "foo-image"),
			oneImageXML(2, oneImageStateReady, 0, "app-image-1"),
		)},
	})
	res, err := moduleOneImageInfo(context.Background(), conn, map[string]any{
		"ids": []any{"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	images, _ := res.Extra["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images = %v", res.Extra["images"])
	}
}

func TestModuleOneImageInfoByNameRegex(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneimage": {RC: 0},
		"oneimage list -x": {RC: 0, Stdout: oneImagePoolXML(
			oneImageXML(1, oneImageStateReady, 0, "foo-image"),
			oneImageXML(2, oneImageStateReady, 0, "app-image-1"),
			oneImageXML(3, oneImageStateReady, 0, "app-image-2"),
		)},
	})
	res, err := moduleOneImageInfo(context.Background(), conn, map[string]any{
		"name": "~app-image-.*",
	})
	if err != nil {
		t.Fatal(err)
	}
	images, _ := res.Extra["images"].([]any)
	if len(images) != 2 {
		t.Fatalf("images = %v", res.Extra["images"])
	}
}

func TestModuleOneImageInfoByNameCaseInsensitive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v oneimage": {RC: 0},
		"oneimage list -x": {RC: 0, Stdout: oneImagePoolXML(
			oneImageXML(1, oneImageStateReady, 0, "Foo-Image"),
		)},
	})
	res, err := moduleOneImageInfo(context.Background(), conn, map[string]any{
		"name": "~*foo-image",
	})
	if err != nil {
		t.Fatal(err)
	}
	images, _ := res.Extra["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images = %v", res.Extra["images"])
	}
}
