package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const influxRPShowCmd = `INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'SHOW RETENTION POLICIES ON "mydb"'`

func TestModuleInfluxdbRetentionPolicyCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxRPShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"policies","columns":["name","duration","shardGroupDuration","replicaN","default"],"values":[]}]}]}`},
		`INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'CREATE RETENTION POLICY "test" ON "mydb" DURATION 1h REPLICATION 1'`: {RC: 0, Stdout: `{"results":[{}]}`},
	})
	res, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{
		"database_name": "mydb", "policy_name": "test", "duration": "1h", "replication": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbRetentionPolicyUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxRPShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"policies","columns":["name","duration","shardGroupDuration","replicaN","default"],"values":[["test","1h0m0s","1h0m0s",1,false]]}]}]}`},
	})
	res, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{
		"database_name": "mydb", "policy_name": "test", "duration": "1h", "replication": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbRetentionPolicyAlterOnReplicationMismatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxRPShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"policies","columns":["name","duration","shardGroupDuration","replicaN","default"],"values":[["test","1h0m0s","1h0m0s",1,false]]}]}]}`},
		`INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'ALTER RETENTION POLICY "test" ON "mydb" DURATION 1h REPLICATION 2'`: {RC: 0, Stdout: `{"results":[{}]}`},
	})
	res, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{
		"database_name": "mydb", "policy_name": "test", "duration": "1h", "replication": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbRetentionPolicyDrop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxRPShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"policies","columns":["name","duration","shardGroupDuration","replicaN","default"],"values":[["test","1h0m0s","1h0m0s",1,false]]}]}]}`},
		`INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'DROP RETENTION POLICY "test" ON "mydb"'`: {RC: 0, Stdout: `{"results":[{}]}`},
	})
	res, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{
		"database_name": "mydb", "policy_name": "test", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbRetentionPolicyDropAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxRPShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"policies","columns":["name","duration","shardGroupDuration","replicaN","default"],"values":[]}]}]}`},
	})
	res, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{
		"database_name": "mydb", "policy_name": "test", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbRetentionPolicyInvalidDuration(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxRPShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"policies","columns":["name","duration","shardGroupDuration","replicaN","default"],"values":[]}]}]}`},
	})
	res, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{
		"database_name": "mydb", "policy_name": "test", "duration": "notaduration", "replication": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an invalid duration literal")
	}
}

func TestModuleInfluxdbRetentionPolicyTooShort(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxRPShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"policies","columns":["name","duration","shardGroupDuration","replicaN","default"],"values":[]}]}]}`},
	})
	res, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{
		"database_name": "mydb", "policy_name": "test", "duration": "30m", "replication": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a duration shorter than 1h")
	}
}

func TestModuleInfluxdbRetentionPolicyMissingDuration(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxRPShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"policies","columns":["name","duration","shardGroupDuration","replicaN","default"],"values":[]}]}]}`},
	})
	if _, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{
		"database_name": "mydb", "policy_name": "test", "replication": 1,
	}); err == nil {
		t.Fatal("want error for missing duration")
	}
}

func TestModuleInfluxdbRetentionPolicyMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{"policy_name": "test"}); err == nil {
		t.Fatal("want error for missing database_name")
	}
	if _, err := moduleInfluxdbRetentionPolicy(context.Background(), conn, map[string]any{"database_name": "mydb"}); err == nil {
		t.Fatal("want error for missing policy_name")
	}
}
