package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewaySecurityGroupInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v scw": {RC: 0},
		"scw instance security-group list zone=fr-par-1 -o json": {
			RC: 0, Stdout: `[{"id":"sg1","name":"default"}]`,
		},
	})
	res, err := moduleScalewaySecurityGroupInfo(context.Background(), conn, map[string]any{"region": "par1"})
	if err != nil {
		t.Fatal(err)
	}
	groups, ok := res.Extra["security_groups"].([]map[string]any)
	if !ok || len(groups) != 1 || groups[0]["id"] != "sg1" {
		t.Fatalf("security_groups = %+v", res.Extra["security_groups"])
	}
}
