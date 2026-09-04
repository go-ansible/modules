package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayServerInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance server list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"srv1","name":"web-1"}]`,
		},
	})
	res, err := moduleScalewayServerInfo(context.Background(), conn, map[string]any{"region": "par1"})
	if err != nil {
		t.Fatal(err)
	}
	servers, ok := res.Extra["servers"].([]map[string]any)
	if !ok || len(servers) != 1 || servers[0]["id"] != "srv1" {
		t.Fatalf("servers = %+v", res.Extra["servers"])
	}
}
