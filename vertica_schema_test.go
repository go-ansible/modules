package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func verticaSchemaFactsQueries(schema string) (string, string) {
	q1 := "select schema_name, schema_owner from schemata where not is_system_schema " +
		"and schema_name not in ('public', 'TxtIndex') and schema_name ilike " + verticaQuoteLiteral(schema)
	q2 := "select g.object_name, r.name, lower(g.privileges_description) from roles r join grants g " +
		"on g.grantee_id = r.role_id and g.object_type='SCHEMA' and g.privileges_description like '%USAGE%' " +
		"and g.grantee not in ('public', 'dbadmin') and g.object_name ilike " + verticaQuoteLiteral(schema)
	return q1, q2
}

func TestModuleVerticaSchemaCreate(t *testing.T) {
	q1, q2 := verticaSchemaFactsQueries("myschema")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(q1):                       {RC: 0, Stdout: ""},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(q2):                       {RC: 0, Stdout: ""},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote("create schema myschema"): {RC: 0},
	})
	res, err := moduleVerticaSchema(context.Background(), conn, map[string]any{"schema": "myschema"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleVerticaSchemaAbsentAlreadyGone(t *testing.T) {
	q1, q2 := verticaSchemaFactsQueries("myschema")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(q1): {RC: 0, Stdout: ""},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(q2): {RC: 0, Stdout: ""},
	})
	res, err := moduleVerticaSchema(context.Background(), conn, map[string]any{"schema": "myschema", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleVerticaSchemaOwnerChangeRefused(t *testing.T) {
	q1, q2 := verticaSchemaFactsQueries("myschema")
	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(q1): {RC: 0, Stdout: "myschema|origowner|2024-01-01\n"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(q2): {RC: 0, Stdout: ""},
	})
	res, err := moduleVerticaSchema(context.Background(), conn, map[string]any{"schema": "myschema", "owner": "newowner"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for owner change")
	}
}

func TestModuleVerticaSchemaMissingSchema(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleVerticaSchema(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing schema")
	}
}
