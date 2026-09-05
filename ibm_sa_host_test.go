package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIbmSaHostCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s host_list host=host_name": {RC: 1},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y host_define host=host_name":  {RC: 0},
	})
	args := ibmSaArgs(map[string]any{"host": "host_name", "state": "present"})
	res, err := moduleIbmSaHost(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIbmSaHostDeleteNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s host_list host=host_name": {RC: 1},
	})
	args := ibmSaArgs(map[string]any{"host": "host_name", "state": "absent"})
	res, err := moduleIbmSaHost(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
