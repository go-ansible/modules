package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIbmSaHostPortsAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s host_list_ports host=test_host": {
			RC: 0, Stdout: "port_name\nother_iqn",
		},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y host_add_port host=test_host iscsi_name=iqn.1994-05.com.example": {RC: 0},
	})
	args := ibmSaArgs(map[string]any{"host": "test_host", "iscsi_name": "iqn.1994-05.com.example", "state": "present"})
	res, err := moduleIbmSaHostPorts(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIbmSaHostPortsAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s host_list_ports host=test_host": {
			RC: 0, Stdout: "port_name\niqn.1994-05.com.example",
		},
	})
	args := ibmSaArgs(map[string]any{"host": "test_host", "iscsi_name": "iqn.1994-05.com.example", "state": "present"})
	res, err := moduleIbmSaHostPorts(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
