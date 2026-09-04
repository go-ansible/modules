package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayVolumeInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance volume list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"vol1","name":"data"}]`,
		},
	})
	res, err := moduleScalewayVolumeInfo(context.Background(), conn, map[string]any{"region": "par1"})
	if err != nil {
		t.Fatal(err)
	}
	vols, ok := res.Extra["volumes"].([]map[string]any)
	if !ok || len(vols) != 1 || vols[0]["id"] != "vol1" {
		t.Fatalf("volumes = %+v", res.Extra["volumes"])
	}
}
