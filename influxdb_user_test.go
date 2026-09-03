package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const influxUsersShowCmd = "INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'SHOW USERS'"

func TestModuleInfluxdbUserCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxUsersShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"users","columns":["user","admin"],"values":[]}]}]}`},
		`INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'CREATE USER "john" WITH PASSWORD '"'"'s3cr3t'"'"''`: {RC: 0, Stdout: `{"results":[{}]}`},
	})
	res, err := moduleInfluxdbUser(context.Background(), conn, map[string]any{
		"user_name": "john", "user_password": "s3cr3t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbUserGrantAdmin(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxUsersShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"users","columns":["user","admin"],"values":[["john",false]]}]}]}`},
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'GRANT ALL PRIVILEGES TO \"john\"'": {RC: 0, Stdout: `{"results":[{}]}`},
	})
	res, err := moduleInfluxdbUser(context.Background(), conn, map[string]any{
		"user_name": "john", "admin": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbUserDrop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxUsersShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"users","columns":["user","admin"],"values":[["john",false]]}]}]}`},
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'DROP USER \"john\"'": {RC: 0, Stdout: `{"results":[{}]}`},
	})
	res, err := moduleInfluxdbUser(context.Background(), conn, map[string]any{
		"user_name": "john", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbUserDropAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxUsersShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"users","columns":["user","admin"],"values":[]}]}]}`},
	})
	res, err := moduleInfluxdbUser(context.Background(), conn, map[string]any{
		"user_name": "john", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbUserGrantsReconciled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		influxUsersShowCmd: {RC: 0, Stdout: `{"results":[{"series":[{"name":"users","columns":["user","admin"],"values":[["john",false]]}]}]}`},
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'SHOW GRANTS FOR \"john\"'": {
			RC: 0, Stdout: `{"results":[{"series":[{"name":"","columns":["database","privilege"],"values":[["collectd","WRITE"]]}]}]}`,
		},
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'REVOKE WRITE ON \"collectd\" FROM \"john\"'": {RC: 0, Stdout: `{"results":[{}]}`},
		"INFLUX_PASSWORD=root influx -host localhost -port 8086 -username root -format json -execute 'GRANT READ ON \"graphite\" TO \"john\"'":     {RC: 0, Stdout: `{"results":[{}]}`},
	})
	res, err := moduleInfluxdbUser(context.Background(), conn, map[string]any{
		"user_name": "john",
		"grants": []any{
			map[string]any{"database": "graphite", "privilege": "READ"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleInfluxdbUserMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleInfluxdbUser(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing user_name")
	}
}
