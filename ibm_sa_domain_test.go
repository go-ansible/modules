package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func ibmSaArgs(extra map[string]any) map[string]any {
	args := map[string]any{
		"username":  "admin",
		"password":  "secret",
		"endpoints": "hostdev-system",
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func TestModuleIbmSaDomainCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s domain_list domain=domain_name":                 {RC: 1},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y domain_create domain=domain_name size=domain_size": {RC: 0},
	})
	args := ibmSaArgs(map[string]any{"domain": "domain_name", "size": "domain_size", "state": "present"})
	res, err := moduleIbmSaDomain(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIbmSaDomainAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s domain_list domain=domain_name": {
			RC: 0, Stdout: "name\ndomain_name",
		},
	})
	args := ibmSaArgs(map[string]any{"domain": "domain_name", "state": "present"})
	res, err := moduleIbmSaDomain(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIbmSaDomainDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 0},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y -s domain_list domain=domain_name": {
			RC: 0, Stdout: "name\ndomain_name",
		},
		"XIV_XCLIPASSWORD=secret xcli -u admin -m hostdev-system -y domain_delete domain=domain_name": {RC: 0},
	})
	args := ibmSaArgs(map[string]any{"domain": "domain_name", "state": "absent"})
	res, err := moduleIbmSaDomain(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIbmSaDomainMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v xcli": {RC: 1},
	})
	args := ibmSaArgs(map[string]any{"domain": "domain_name"})
	res, err := moduleIbmSaDomain(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed (xcli missing)", res)
	}
}
