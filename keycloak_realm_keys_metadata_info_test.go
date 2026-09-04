package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakRealmKeysMetadataInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v kcadm.sh": {RC: 0},
		"kcadm.sh get keys -r master": {
			RC: 0, Stdout: `{"active":{"RS256":"abc"},"keys":[{"kid":"abc","algorithm":"RS256"}]}`,
		},
	})
	res, err := moduleKeycloakRealmKeysMetadataInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	meta := res.Extra["keys_metadata"].(map[string]any)
	active := meta["active"].(map[string]any)
	if active["RS256"] != "abc" {
		t.Fatalf("keys_metadata = %#v", meta)
	}
}
