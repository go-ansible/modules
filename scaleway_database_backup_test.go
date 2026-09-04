package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleScalewayDatabaseBackupCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"scw rdb backup create instance-id=inst-1 name=bk database-name=db1 region=fr-par -o json": {
			RC: 0, Stdout: `{"id":"bk-1","name":"bk","database_name":"db1"}`,
		},
	})
	res, err := moduleScalewayDatabaseBackup(context.Background(), conn, map[string]any{
		"name": "bk", "database_name": "db1", "instance_id": "inst-1", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayDatabaseBackupPresentIdempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"scw rdb backup get bk-1 region=fr-par -o json": {
			RC: 0, Stdout: `{"id":"bk-1","name":"bk","expires_at":null}`,
		},
	})
	res, err := moduleScalewayDatabaseBackup(context.Background(), conn, map[string]any{
		"id": "bk-1", "name": "bk", "database_name": "db1", "instance_id": "inst-1", "region": "fr-par",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayDatabaseBackupDeleteExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"scw rdb backup get bk-1 region=fr-par -o json": {RC: 0, Stdout: `{"id":"bk-1","name":"bk"}`},
		"scw rdb backup delete bk-1 region=fr-par":      {RC: 0},
	})
	res, err := moduleScalewayDatabaseBackup(context.Background(), conn, map[string]any{
		"id": "bk-1", "region": "fr-par", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayDatabaseBackupDeleteMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"scw rdb backup get bk-1 region=fr-par -o json": {RC: 1, Stderr: "error: not found (404)"},
	})
	res, err := moduleScalewayDatabaseBackup(context.Background(), conn, map[string]any{
		"id": "bk-1", "region": "fr-par", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayDatabaseBackupExportedNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"scw rdb backup get bk-1 region=fr-par -o json": {RC: 1, Stderr: "404 not found"},
	})
	res, err := moduleScalewayDatabaseBackup(context.Background(), conn, map[string]any{
		"id": "bk-1", "region": "fr-par", "state": "exported",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("expected Failed, res = %+v", res)
	}
}

func TestModuleScalewayDatabaseBackupRestored(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"scw rdb backup get bk-1 region=fr-par -o json": {RC: 0, Stdout: `{"id":"bk-1"}`},
		"scw rdb backup restore bk-1 instance-id=inst-1 database-name=db2 region=fr-par -o json": {
			RC: 0, Stdout: `{"id":"bk-1","database_name":"db2"}`,
		},
	})
	res, err := moduleScalewayDatabaseBackup(context.Background(), conn, map[string]any{
		"id": "bk-1", "instance_id": "inst-1", "database_name": "db2", "region": "fr-par", "state": "restored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleScalewayDatabaseBackupAbsentRequiresID(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleScalewayDatabaseBackup(context.Background(), conn, map[string]any{
		"region": "fr-par", "state": "absent",
	})
	if err == nil {
		t.Fatal("expected error when id is missing for state=absent")
	}
}
