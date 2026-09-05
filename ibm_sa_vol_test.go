package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIbmSaVolCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s vol_list vol=volume_name":                       {RC: 1},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y vol_create pool=pool_name size=17 vol=volume_name": {RC: 0},
	})
	args := ibmSaArgs(map[string]any{"vol": "volume_name", "pool": "pool_name", "size": "17", "state": "present"})
	res, err := moduleIbmSaVol(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIbmSaVolDeleteNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s vol_list vol=volume_name": {RC: 1},
	})
	args := ibmSaArgs(map[string]any{"vol": "volume_name", "state": "absent"})
	res, err := moduleIbmSaVol(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
