package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleVerticaSchema implements Ansible's `vertica_schema`
// (community.general) module: adds or removes a Vertica database
// schema and, optionally, creates usage/create-privileged roles for
// it.
//
// Deviation from real vertica_schema: real vertica_schema.py talks to
// Vertica through pyodbc; this port shells out to vsql instead — see
// vertica.go's own doc comments for the full rationale shared by every
// vertica_* module in this batch.
//
// Args: schema (required, alias name); usage_roles (string, comma
// separated, alias usage_role) — roles to create (if missing) and
// grant `usage` on the schema; create_roles (string, comma separated,
// alias create_role) — roles to create (if missing) and grant both
// `usage` and `create` on the schema; owner; state (present|absent,
// default present); db, cluster (default localhost), port (default
// "5433"), login_user (default dbadmin), login_password.
//
// Idempotency: reads the schema's current owner and
// usage_roles/create_roles (via `grants` joined to `roles`, matching
// real vertica_schema's own query) and only issues DDL when they
// differ. Changing an existing schema's owner is refused (Fail),
// matching real vertica_schema's own NotSupportedError for that case
// — real vertica_schema, and this port, only ever set the owner via
// `create schema ... authorization ...` at creation time.
// state=absent revokes every usage_roles/create_roles role (dropping
// each with `cascade`, matching real vertica_schema's own
// update_roles) then `drop schema ... restrict`; a no-op if the schema
// does not exist.
func moduleVerticaSchema(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	schema := argString(args, "schema", argString(args, "name", ""))
	if schema == "" {
		return Result{}, errArg("vertica_schema: schema (or name) is required")
	}
	usageRoles := verticaSplitList(argString(args, "usage_roles", argString(args, "usage_role", "")))
	createRoles := verticaSplitList(argString(args, "create_roles", argString(args, "create_role", "")))
	owner := argString(args, "owner", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("vertica_schema: state must be present or absent, got %q", state)
	}

	exists, facts, err := verticaSchemaFacts(ctx, conn, args, schema)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(schema + " already absent"), nil
		}
		if err := verticaUpdateSchemaRoles(ctx, conn, args, schema, facts.usageRoles, nil, facts.createRoles, nil); err != nil {
			return Result{}, err
		}
		res, err := verticaVsql(ctx, conn, args, "drop schema "+schema+" restrict")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("vertica_schema: dropping schema failed (likely due to dependencies): " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(schema + " dropped"), nil
	}

	// state == "present"
	if !exists {
		cmd := "create schema " + schema
		if owner != "" {
			cmd += " authorization " + owner
		}
		res, err := verticaVsql(ctx, conn, args, cmd)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("vertica_schema: unable to create schema " + schema + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		if err := verticaUpdateSchemaRoles(ctx, conn, args, schema, nil, usageRoles, nil, createRoles); err != nil {
			return Result{}, err
		}
		return Changed(schema + " created"), nil
	}

	if owner != "" && !strings.EqualFold(owner, facts.owner) {
		return Fail("vertica_schema: changing schema owner is not supported (current owner: " + facts.owner + ")"), nil
	}

	if !verticaSameSet(usageRoles, facts.usageRoles) || !verticaSameSet(createRoles, facts.createRoles) {
		if err := verticaUpdateSchemaRoles(ctx, conn, args, schema, facts.usageRoles, usageRoles, facts.createRoles, createRoles); err != nil {
			return Result{}, err
		}
		return Changed(schema + " roles updated"), nil
	}
	return Ok(schema + " unchanged"), nil
}

type verticaSchemaRow struct {
	owner                   string
	usageRoles, createRoles []string
}

// verticaSchemaFacts reads a schema's current owner and
// usage_roles/create_roles, matching real vertica_schema's own
// get_schema_facts (two queries: one for name/owner, one for the
// USAGE-privileged roles granted on it, split into usage vs create
// based on whether the grant's privileges_description also mentions
// "create").
func verticaSchemaFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any, schema string) (bool, verticaSchemaRow, error) {
	q := "select schema_name, schema_owner from schemata where not is_system_schema " +
		"and schema_name not in ('public', 'TxtIndex') and schema_name ilike " + verticaQuoteLiteral(schema)
	res, err := verticaVsql(ctx, conn, args, q)
	if err != nil {
		return false, verticaSchemaRow{}, err
	}
	if res.RC != 0 {
		return false, verticaSchemaRow{}, errArg("vertica_schema: unable to read schema facts: %s", strings.TrimSpace(res.Stderr))
	}
	rows := verticaParseRows(res.Stdout)
	if len(rows) == 0 {
		return false, verticaSchemaRow{}, nil
	}
	owner := ""
	if len(rows[0]) > 1 {
		owner = rows[0][1]
	}

	q2 := "select g.object_name, r.name, lower(g.privileges_description) from roles r join grants g " +
		"on g.grantee_id = r.role_id and g.object_type='SCHEMA' and g.privileges_description like '%USAGE%' " +
		"and g.grantee not in ('public', 'dbadmin') and g.object_name ilike " + verticaQuoteLiteral(schema)
	res2, err := verticaVsql(ctx, conn, args, q2)
	if err != nil {
		return false, verticaSchemaRow{}, err
	}
	if res2.RC != 0 {
		return false, verticaSchemaRow{}, errArg("vertica_schema: unable to read schema role grants: %s", strings.TrimSpace(res2.Stderr))
	}
	var usage, create []string
	for _, row := range verticaParseRows(res2.Stdout) {
		if len(row) < 3 {
			continue
		}
		roleName, priv := row[1], row[2]
		if strings.Contains(priv, "create") {
			create = append(create, roleName)
		} else {
			usage = append(usage, roleName)
		}
	}
	return true, verticaSchemaRow{owner: owner, usageRoles: usage, createRoles: create}, nil
}

// verticaUpdateSchemaRoles matches real vertica_schema's own
// update_roles: any role present in the existing sets but not the
// required sets is dropped entirely (cascade), any role newly required
// (in either set) is created and granted usage, and any role newly
// required in create_required is additionally granted create; a role
// dropped from create_required but still in usage_required is only
// revoked create (not dropped).
func verticaUpdateSchemaRoles(ctx context.Context, conn remoteexec.Connection, args map[string]any, schema string, existingUsage, requiredUsage, existingCreate, requiredCreate []string) error {
	existingAll := verticaUnion(existingUsage, existingCreate)
	requiredAll := verticaUnion(requiredUsage, requiredCreate)

	for _, r := range existingAll {
		if !containsStr(requiredAll, r) {
			if res, err := verticaVsql(ctx, conn, args, "drop role "+r+" cascade"); err != nil {
				return err
			} else if res.RC != 0 {
				return errArg("vertica_schema: unable to drop role %s: %s", r, strings.TrimSpace(res.Stderr))
			}
		}
	}
	for _, r := range existingCreate {
		if !containsStr(requiredCreate, r) {
			if res, err := verticaVsql(ctx, conn, args, "revoke create on schema "+schema+" from "+r); err != nil {
				return err
			} else if res.RC != 0 {
				return errArg("vertica_schema: unable to revoke create on schema %s from %s: %s", schema, r, strings.TrimSpace(res.Stderr))
			}
		}
	}
	for _, r := range requiredAll {
		if !containsStr(existingAll, r) {
			if res, err := verticaVsql(ctx, conn, args, "create role "+r); err != nil {
				return err
			} else if res.RC != 0 {
				return errArg("vertica_schema: unable to create role %s: %s", r, strings.TrimSpace(res.Stderr))
			}
			if res, err := verticaVsql(ctx, conn, args, "grant usage on schema "+schema+" to "+r); err != nil {
				return err
			} else if res.RC != 0 {
				return errArg("vertica_schema: unable to grant usage on schema %s to %s: %s", schema, r, strings.TrimSpace(res.Stderr))
			}
		}
	}
	for _, r := range requiredCreate {
		if !containsStr(existingCreate, r) {
			if res, err := verticaVsql(ctx, conn, args, "grant create on schema "+schema+" to "+r); err != nil {
				return err
			} else if res.RC != 0 {
				return errArg("vertica_schema: unable to grant create on schema %s to %s: %s", schema, r, strings.TrimSpace(res.Stderr))
			}
		}
	}
	return nil
}

// verticaUnion returns the set union of a and b, without duplicates.
func verticaUnion(a, b []string) []string {
	var out []string
	for _, v := range a {
		if !containsStr(out, v) {
			out = append(out, v)
		}
	}
	for _, v := range b {
		if !containsStr(out, v) {
			out = append(out, v)
		}
	}
	return out
}
