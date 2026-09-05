package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const emcSgListOutput = `Storage Group Name:    sg01
Storage Group UID:     10:00:00:00:C9:39:40:C8:10:00:00:00:C9:39:40:C8

HBA/SP Pairs:

  HBA UID                                          SP Name     SPPort
  -------                                          -------     ------
  20:00:00:00:C9:39:40:C8:10:00:00:00:C9:39:40:C8 SP A
  0

HLU/ALU Pairs:

  HLU Number     ALU Number
  ----------     ----------
  0                15
  1                14

Shareable:             YES
`

func TestModuleEmcVnxSgMemberAddNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"naviseccli -h sp1a.fqdn storagegroup -list -gname sg01":                   {RC: 0, Stdout: emcSgListOutput},
		"naviseccli -h sp1a.fqdn storagegroup -addhlu -gname sg01 -hlu 2 -alu 100": {RC: 0, Stdout: ""},
	})
	args := map[string]any{
		"name": "sg01", "sp_address": "sp1a.fqdn", "sp_user": "sysadmin", "sp_password": "sysadmin",
		"lunid": 100, "state": "present",
	}
	res, err := moduleEmcVnxSgMember(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["hluid"] != 2 {
		t.Fatalf("hluid = %v, want 2", res.Extra["hluid"])
	}
}

func TestModuleEmcVnxSgMemberAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"naviseccli -h sp1a.fqdn storagegroup -list -gname sg01": {RC: 0, Stdout: emcSgListOutput},
	})
	args := map[string]any{
		"name": "sg01", "sp_address": "sp1a.fqdn", "lunid": 15, "state": "present",
	}
	res, err := moduleEmcVnxSgMember(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["hluid"] != 0 {
		t.Fatalf("hluid = %v, want 0", res.Extra["hluid"])
	}
}

func TestModuleEmcVnxSgMemberRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"naviseccli -h sp1a.fqdn storagegroup -list -gname sg01":             {RC: 0, Stdout: emcSgListOutput},
		"naviseccli -h sp1a.fqdn storagegroup -removehlu -gname sg01 -hlu 0": {RC: 0},
	})
	args := map[string]any{
		"name": "sg01", "sp_address": "sp1a.fqdn", "lunid": 15, "state": "absent",
	}
	res, err := moduleEmcVnxSgMember(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleEmcVnxSgMemberRemoveAbsentAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"naviseccli -h sp1a.fqdn storagegroup -list -gname sg01": {RC: 0, Stdout: emcSgListOutput},
	})
	args := map[string]any{
		"name": "sg01", "sp_address": "sp1a.fqdn", "lunid": 999, "state": "absent",
	}
	res, err := moduleEmcVnxSgMember(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleEmcVnxSgMemberNoSuchGroup(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"naviseccli -h sp1a.fqdn storagegroup -list -gname sg01": {RC: 1, Stderr: "No such storage group"},
	})
	args := map[string]any{
		"name": "sg01", "sp_address": "sp1a.fqdn", "lunid": 100, "state": "present",
	}
	res, err := moduleEmcVnxSgMember(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v, want Failed", res)
	}
}
