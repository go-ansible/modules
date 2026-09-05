package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIbmSaPoolCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s pool_list pool=pool_name":         {RC: 1},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y pool_create pool=pool_name size=300": {RC: 0},
	})
	args := ibmSaArgs(map[string]any{"pool": "pool_name", "size": "300", "state": "present"})
	res, err := moduleIbmSaPool(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIbmSaPoolDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s pool_list pool=pool_name": {
			RC: 0, Stdout: "name\npool_name",
		},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y pool_delete pool=pool_name": {RC: 0},
	})
	args := ibmSaArgs(map[string]any{"pool": "pool_name", "state": "absent"})
	res, err := moduleIbmSaPool(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
