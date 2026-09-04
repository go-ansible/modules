package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayImageInfoList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"scw instance image list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"img-1","name":"Debian Bookworm"},{"id":"img-2","name":"Ubuntu"}]`,
		},
	})
	res, err := moduleScalewayImageInfo(context.Background(), conn, map[string]any{"region": "par1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	images, ok := res.Extra["scaleway_image_info"].([]map[string]any)
	if !ok || len(images) != 2 {
		t.Fatalf("scaleway_image_info = %+v", res.Extra["scaleway_image_info"])
	}
}

func TestModuleScalewayImageInfoBadRegion(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleScalewayImageInfo(context.Background(), conn, map[string]any{"region": "bogus"})
	if err == nil {
		t.Fatal("expected error for bad region")
	}
}
