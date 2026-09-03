package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleVerticaInfo(t *testing.T) {
	schemasQ := "select schema_name, schema_owner, create_time from schemata " +
		"where not is_system_schema and schema_name not in ('public')"
	usersQ := "select u.user_name, u.is_locked, p.acctexpired, u.profile_name, u.resource_pool, u.all_roles, u.default_roles " +
		"from users u join password_auditor p on p.user_id = u.user_id where not u.is_super_user"
	rolesQ := "select name, assigned_roles from roles"
	configQ := "select parameter_name, current_value, default_value from configuration_parameters where node_name = 'ALL'"
	nodesQ := "select node_name, node_address, export_address, node_state, node_type, catalog_path from nodes"

	conn := newFakeConn(map[string]remoteexec.Result{
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(schemasQ): {RC: 0, Stdout: "s1|owner1|2024-01-01\n"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(usersQ):   {RC: 0, Stdout: "u1|f|f|prof|pool|r1,r2|r1\n"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(rolesQ):   {RC: 0, Stdout: "r1|\n"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(configQ):  {RC: 0, Stdout: "p1|v1|d1\n"},
		"vsql -h localhost -p 5433 -U dbadmin -X -A -t -c " + shellQuote(nodesQ):   {RC: 0, Stdout: "n1|10.0.0.1|10.0.0.2|UP|PERMANENT|/data\n"},
	})
	res, err := moduleVerticaInfo(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	schemas, ok := res.Extra["vertica_schemas"].(map[string]any)
	if !ok || schemas["s1"] == nil {
		t.Fatalf("vertica_schemas = %#v", res.Extra["vertica_schemas"])
	}
	users, ok := res.Extra["vertica_users"].(map[string]any)
	if !ok || users["u1"] == nil {
		t.Fatalf("vertica_users = %#v", res.Extra["vertica_users"])
	}
	roles, ok := res.Extra["vertica_roles"].(map[string]any)
	if !ok || roles["r1"] == nil {
		t.Fatalf("vertica_roles = %#v", res.Extra["vertica_roles"])
	}
	config, ok := res.Extra["vertica_configuration"].(map[string]any)
	if !ok || config["p1"] == nil {
		t.Fatalf("vertica_configuration = %#v", res.Extra["vertica_configuration"])
	}
	nodes, ok := res.Extra["vertica_nodes"].(map[string]any)
	if !ok || nodes["10.0.0.1"] == nil {
		t.Fatalf("vertica_nodes = %#v", res.Extra["vertica_nodes"])
	}
}
