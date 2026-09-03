package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLxdStorageVolumeInfoMissingPool(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxdStorageVolumeInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing pool")
	}
}

func TestModuleLxdStorageVolumeInfoAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET '/1.0/storage-pools/default/volumes?recursion=1'": {RC: 0, Stdout: `[
			{"name":"my-volume","type":"custom","content_type":"filesystem","description":"My custom volume","config":{"size":"10GiB"},"location":"none","used_by":[]},
			{"name":"container1","type":"container","content_type":"filesystem","description":"","config":{},"location":"none","used_by":["/1.0/instances/container1"]}
		]`},
	})
	res, err := moduleLxdStorageVolumeInfo(context.Background(), conn, map[string]any{"pool": "default"})
	if err != nil {
		t.Fatal(err)
	}
	vols := res.Extra["storage_volumes"].([]any)
	if len(vols) != 2 {
		t.Fatalf("got %d volumes, want 2", len(vols))
	}
}

func TestModuleLxdStorageVolumeInfoByTypeAndName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET '/1.0/storage-pools/default/volumes?recursion=1'": {RC: 0, Stdout: `[
			{"name":"my-volume","type":"custom","content_type":"filesystem","description":"","config":{},"location":"none","used_by":[]},
			{"name":"container1","type":"container","content_type":"filesystem","description":"","config":{},"location":"none","used_by":[]}
		]`},
	})
	res, err := moduleLxdStorageVolumeInfo(context.Background(), conn, map[string]any{"pool": "default", "type": "custom"})
	if err != nil {
		t.Fatal(err)
	}
	vols := res.Extra["storage_volumes"].([]any)
	if len(vols) != 1 {
		t.Fatalf("got %d volumes, want 1", len(vols))
	}
}
