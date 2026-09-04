package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewaySnapshotInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance snapshot list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"snap1","name":"backup"}]`,
		},
	})
	res, err := moduleScalewaySnapshotInfo(context.Background(), conn, map[string]any{"region": "par1"})
	if err != nil {
		t.Fatal(err)
	}
	snaps, ok := res.Extra["snapshots"].([]map[string]any)
	if !ok || len(snaps) != 1 || snaps[0]["id"] != "snap1" {
		t.Fatalf("snapshots = %+v", res.Extra["snapshots"])
	}
}
