package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSmartosImageInfo(t *testing.T) {
	out := `[{"manifest":{"uuid":"70e3ae72-96b6-11e6-9056-9737fd4d0764","name":"base","version":"1.0.0"},"clones":2,"source":"https://datasets.project-fifo.net","zpool":"zones"}]`
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm list -j": {RC: 0, Stdout: out},
	})
	res, err := moduleSmartosImageInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want never changed")
	}
	images, ok := res.Extra["smartos_images"].(map[string]any)
	if !ok {
		t.Fatalf("smartos_images not a map, extra = %+v", res.Extra)
	}
	img, ok := images["70e3ae72-96b6-11e6-9056-9737fd4d0764"].(map[string]any)
	if !ok {
		t.Fatalf("missing image entry, images = %+v", images)
	}
	if img["name"] != "base" {
		t.Fatalf("name = %v", img["name"])
	}
	if img["clones"] != float64(2) {
		t.Fatalf("clones = %v", img["clones"])
	}
	if img["zpool"] != "zones" {
		t.Fatalf("zpool = %v", img["zpool"])
	}
}

func TestModuleSmartosImageInfoWithFilters(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm list -j os=linux state=active public=false": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleSmartosImageInfo(context.Background(), conn, map[string]any{"filters": "os=linux state=active public=false"})
	if err != nil {
		t.Fatal(err)
	}
	images := res.Extra["smartos_images"].(map[string]any)
	if len(images) != 0 {
		t.Fatalf("images = %+v, want empty", images)
	}
}

func TestModuleSmartosImageInfoFailureIsStillOk(t *testing.T) {
	// Real smartos_image_info's own module calls exit_json (success),
	// not fail_json, when imgadm list fails — see the doc comment.
	conn := newFakeConn(map[string]remoteexec.Result{
		"imgadm list -j": {RC: 1, Stderr: "imgadm: command not found"},
	})
	res, err := moduleSmartosImageInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want Ok (not Failed), matching real smartos_image_info's own exit_json-on-error quirk")
	}
	if _, ok := res.Extra["smartos_images"]; ok {
		t.Fatal("want no smartos_images key on failure")
	}
}
