package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleVerticaInfo implements Ansible's `vertica_info`
// (community.general) module: gathers read-only facts about a Vertica
// database — schemas, users, roles, configuration parameters, and
// cluster nodes.
//
// Deviation from real vertica_info: real vertica_info.py talks to
// Vertica through pyodbc; this port shells out to vsql instead — see
// vertica.go's own doc comments for the full rationale shared by every
// vertica_* module in this batch. As with vertica_user.go, the user
// facts this module also gathers omit the `password` (MD5 hash) field
// real vertica_info's own get_user_facts includes, for the same
// catalog-shape-verification reason documented there.
//
// Args: cluster (default localhost); port (default "5433"); db;
// login_user (default dbadmin); login_password.
//
// Real vertica_info nests its five fact sets directly under top-level
// result keys (vertica_schemas, vertica_users, vertica_roles,
// vertica_configuration, vertica_nodes) rather than under
// ansible_facts; this port matches that shape exactly, putting each
// under the identically-named key in Extra. Never reports Changed,
// matching real vertica_info's own unconditional
// `module.exit_json(changed=False, ...)`.
func moduleVerticaInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	schemas, err := verticaInfoSchemas(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	users, err := verticaInfoUsers(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	roles, err := verticaInfoRoles(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	config, err := verticaInfoConfiguration(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	nodes, err := verticaInfoNodes(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}

	return Ok("").
		WithExtra("vertica_schemas", schemas).
		WithExtra("vertica_users", users).
		WithExtra("vertica_roles", roles).
		WithExtra("vertica_configuration", config).
		WithExtra("vertica_nodes", nodes), nil
}

func verticaInfoSchemas(ctx context.Context, conn remoteexec.Connection, args map[string]any) (map[string]any, error) {
	q := "select schema_name, schema_owner, create_time from schemata " +
		"where not is_system_schema and schema_name not in ('public')"
	res, err := verticaVsql(ctx, conn, args, q)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, errArg("vertica_info: unable to read schema facts: %s", strings.TrimSpace(res.Stderr))
	}
	out := map[string]any{}
	for _, row := range verticaParseRows(res.Stdout) {
		for len(row) < 3 {
			row = append(row, "")
		}
		out[strings.ToLower(row[0])] = map[string]any{
			"name":         row[0],
			"owner":        row[1],
			"create_time":  row[2],
			"usage_roles":  []string{},
			"create_roles": []string{},
		}
	}
	return out, nil
}

func verticaInfoUsers(ctx context.Context, conn remoteexec.Connection, args map[string]any) (map[string]any, error) {
	q := "select u.user_name, u.is_locked, p.acctexpired, u.profile_name, u.resource_pool, u.all_roles, u.default_roles " +
		"from users u join password_auditor p on p.user_id = u.user_id where not u.is_super_user"
	res, err := verticaVsql(ctx, conn, args, q)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, errArg("vertica_info: unable to read user facts: %s", strings.TrimSpace(res.Stderr))
	}
	out := map[string]any{}
	for _, row := range verticaParseRows(res.Stdout) {
		for len(row) < 7 {
			row = append(row, "")
		}
		out[strings.ToLower(row[0])] = map[string]any{
			"name":          row[0],
			"locked":        row[1],
			"expired":       row[2],
			"profile":       row[3],
			"resource_pool": row[4],
			"roles":         verticaParseCatalogList(row[5]),
			"default_roles": verticaParseCatalogList(row[6]),
		}
	}
	return out, nil
}

func verticaInfoRoles(ctx context.Context, conn remoteexec.Connection, args map[string]any) (map[string]any, error) {
	res, err := verticaVsql(ctx, conn, args, "select name, assigned_roles from roles")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, errArg("vertica_info: unable to read role facts: %s", strings.TrimSpace(res.Stderr))
	}
	out := map[string]any{}
	for _, row := range verticaParseRows(res.Stdout) {
		for len(row) < 2 {
			row = append(row, "")
		}
		out[strings.ToLower(row[0])] = map[string]any{
			"name":           row[0],
			"assigned_roles": verticaParseCatalogList(row[1]),
		}
	}
	return out, nil
}

func verticaInfoConfiguration(ctx context.Context, conn remoteexec.Connection, args map[string]any) (map[string]any, error) {
	q := "select parameter_name, current_value, default_value from configuration_parameters where node_name = 'ALL'"
	res, err := verticaVsql(ctx, conn, args, q)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, errArg("vertica_info: unable to read configuration facts: %s", strings.TrimSpace(res.Stderr))
	}
	out := map[string]any{}
	for _, row := range verticaParseRows(res.Stdout) {
		for len(row) < 3 {
			row = append(row, "")
		}
		out[strings.ToLower(row[0])] = map[string]any{
			"parameter_name": row[0],
			"current_value":  row[1],
			"default_value":  row[2],
		}
	}
	return out, nil
}

func verticaInfoNodes(ctx context.Context, conn remoteexec.Connection, args map[string]any) (map[string]any, error) {
	q := "select node_name, node_address, export_address, node_state, node_type, catalog_path from nodes"
	res, err := verticaVsql(ctx, conn, args, q)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, errArg("vertica_info: unable to read node facts: %s", strings.TrimSpace(res.Stderr))
	}
	out := map[string]any{}
	for _, row := range verticaParseRows(res.Stdout) {
		for len(row) < 6 {
			row = append(row, "")
		}
		out[row[1]] = map[string]any{
			"node_name":      row[0],
			"export_address": row[2],
			"node_state":     row[3],
			"node_type":      row[4],
			"catalog_path":   row[5],
		}
	}
	return out, nil
}
