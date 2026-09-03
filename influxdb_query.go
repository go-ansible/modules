package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleInfluxdbQuery implements Ansible's `influxdb_query`
// (community.general) module: runs an arbitrary InfluxQL query against
// a database and returns its rows as facts — read from real
// influxdb_query.py's own AnsibleInfluxDBRead.read_by_query.
//
// Args: query (required); database_name (required); hostname, port,
// username (alias login_username), password (alias login_password),
// ssl, validate_certs — see influxdb_database.go's own influxExecute
// doc comment for the shared influxdb_*.go connection/transport
// substitution.
//
// Always reports Changed=true regardless of whether query actually
// mutated anything, matching real influxdb_query's own
// unconditional `module.exit_json(changed=True, ...)` — a SELECT is
// treated the same as any other statement.
//
// Extra: query_results ([]map[string]any, matching real influxdb_query's
// own RETURN VALUES "list" shape from client.query(query).get_points()
// — never nil, even for zero rows, matching that same real return
// type).
func moduleInfluxdbQuery(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	query, err := requireString(args, "query")
	if err != nil {
		return Result{}, err
	}
	database, err := requireString(args, "database_name")
	if err != nil {
		return Result{}, err
	}

	res, err := influxExecute(ctx, conn, args, database, query)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("influxdb_query: " + strings.TrimSpace(res.Stderr)), nil
	}
	rows, err := influxRows(res.Stdout)
	if err != nil {
		return Fail("influxdb_query: " + err.Error()), nil
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	return Changed("").WithExtra("query_results", rows), nil
}
