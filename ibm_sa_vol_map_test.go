package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIbmSaVolMapCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s vol_mapping_list vol=volume_name": {
			RC: 0, Stdout: "host\nother_host",
		},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y map_vol host=host_name lun=1 vol=volume_name": {RC: 0},
	})
	args := ibmSaArgs(map[string]any{"vol": "volume_name", "lun": "1", "host": "host_name", "state": "present"})
	res, err := moduleIbmSaVolMap(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIbmSaVolMapAlreadyMapped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s vol_mapping_list vol=volume_name": {
			RC: 0, Stdout: "host\nhost_name",
		},
	})
	args := ibmSaArgs(map[string]any{"vol": "volume_name", "host": "host_name", "state": "present"})
	res, err := moduleIbmSaVolMap(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
