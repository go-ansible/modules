package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func ss3parArgs(extra map[string]any) map[string]any {
	args := map[string]any{
		"storage_system_ip":       "10.10.10.1",
		"storage_system_username": "username",
		"storage_system_password": "password",
		"cpg_name":                "sample_cpg",
	}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

func TestModuleSs3parCpgCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ssh username@10.10.10.1 'showcpg sample_cpg'": {RC: 1, Stderr: "no cpg listed"},
		"ssh username@10.10.10.1 'createcpg -f -sdgs 32000 -sdgl 64000 -sdgw 48000 -domain sample_domain -t r6 -ssz 8 -ha mag -devtype FC sample_cpg'": {RC: 0},
	})
	args := ss3parArgs(map[string]any{
		"state": "present", "domain": "sample_domain",
		"growth_increment": "32000 MiB", "growth_limit": "64000 MiB", "growth_warning": "48000 MiB",
		"raid_type": "R6", "set_size": 8, "high_availability": "MAG", "disk_type": "FC",
	})
	res, err := moduleSs3parCpg(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleSs3parCpgAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ssh username@10.10.10.1 'showcpg sample_cpg'": {RC: 0, Stdout: "Id Name Warn%\n 0 sample_cpg -"},
	})
	args := ss3parArgs(map[string]any{"state": "present"})
	res, err := moduleSs3parCpg(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSs3parCpgDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ssh username@10.10.10.1 'showcpg sample_cpg'":      {RC: 0, Stdout: "Id Name Warn%\n 0 sample_cpg -"},
		"ssh username@10.10.10.1 'removecpg -f sample_cpg'": {RC: 0},
	})
	args := ss3parArgs(map[string]any{"state": "absent"})
	res, err := moduleSs3parCpg(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSs3parCpgDeleteNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ssh username@10.10.10.1 'showcpg sample_cpg'": {RC: 1},
	})
	args := ss3parArgs(map[string]any{"state": "absent"})
	res, err := moduleSs3parCpg(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleSs3parCpgNameTooLong(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{})
	args := ss3parArgs(map[string]any{
		"cpg_name": "this-cpg-name-is-definitely-too-long-to-be-valid",
		"state":    "present",
	})
	res, err := moduleSs3parCpg(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
