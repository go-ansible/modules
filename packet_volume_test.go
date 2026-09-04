package modules

import (
	"context"
	"testing"
)

func TestModulePacketVolumeFailsLoud(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := modulePacketVolume(context.Background(), conn, map[string]any{"project_id": "proj-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: metal has no volume support")
	}
}

func TestModulePacketVolumeAttachmentFailsLoud(t *testing.T) {
	conn := newFakeConn(nil)
	args := map[string]any{"project_id": "proj-1", "device": "dev-1", "volume": "vol-1"}
	res, err := modulePacketVolumeAttachment(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: metal has no volume support")
	}
}
