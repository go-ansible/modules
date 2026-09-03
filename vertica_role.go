package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleVerticaRole implements Ansible's `vertica_role`
// (community.general) module: adds or removes a Vertica database role
// and, optionally, assigns other roles to it.
//
// Deviation from real vertica_role: real vertica_role.py talks to
// Vertica through pyodbc (a Vertica ODBC DSN); this port has no ODBC
// driver wired into remoteexec.Connection, so it substitutes shelling
// out to Vertica's own `vsql` CLI client on the target instead — see
// vertica.go's own doc comments (verticaConnArgs/verticaVsql) for the
// full rationale, shared by every vertica_* module in this batch.
//
// Args: role (required, alias name); assigned_roles (string, comma
// separated, alias assigned_role — see verticaSplitList's own doc
// comment for its exact, whitespace-preserving split behavior); state
// (present|absent, default present — real vertica_role also documents
// a "locked" choice in its description but never actually accepts it
// in its own argument_spec (choices: ['present', 'absent']); this port
// matches the real, enforced choice set, not the stale doc line); db,
// cluster (default localhost), port (default "5433"), login_user
// (default dbadmin), login_password — connection options, identical to
// every other vertica_* module's own in this batch.
//
// Idempotency: reads the target role's current assigned_roles via
// `roles` catalog table (ilike role, matching real vertica_role's own
// case-insensitive lookup); state=present creates the role if missing
// then grants every assigned_roles entry (a fresh role has none to
// revoke); if the role already exists, only entries that differ from
// the role's current assigned_roles set are revoked/granted (a normal,
// unset assigned_roles argument leaves existing grants untouched,
// matching real vertica_role's own `if assigned_roles and (...)` — an
// explicit empty list is indistinguishable from "not given" in both).
// state=absent revokes every currently assigned role then `drop role
// ... cascade`; a no-op if the role does not exist.
func moduleVerticaRole(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	role := argString(args, "role", argString(args, "name", ""))
	if role == "" {
		return Result{}, errArg("vertica_role: role (or name) is required")
	}
	assignedRoles := verticaSplitList(argString(args, "assigned_roles", argString(args, "assigned_role", "")))
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("vertica_role: state must be present or absent, got %q", state)
	}

	exists, existingAssigned, err := verticaRoleFacts(ctx, conn, args, role)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(role + " already absent"), nil
		}
		if err := verticaUpdateRoleGrants(ctx, conn, args, role, existingAssigned, nil); err != nil {
			return Result{}, err
		}
		res, err := verticaVsql(ctx, conn, args, "drop role "+role+" cascade")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("vertica_role: unable to drop role " + role + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(role + " dropped"), nil
	}

	// state == "present"
	if !exists {
		res, err := verticaVsql(ctx, conn, args, "create role "+role)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("vertica_role: unable to create role " + role + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		if err := verticaUpdateRoleGrants(ctx, conn, args, role, nil, assignedRoles); err != nil {
			return Result{}, err
		}
		return Changed(role + " created"), nil
	}

	if len(assignedRoles) > 0 && !verticaSameSet(assignedRoles, existingAssigned) {
		if err := verticaUpdateRoleGrants(ctx, conn, args, role, existingAssigned, assignedRoles); err != nil {
			return Result{}, err
		}
		return Changed(role + " roles updated"), nil
	}
	return Ok(role + " unchanged"), nil
}

// verticaRoleFacts reports whether role exists, and (if it does) its
// current assigned_roles list, via the `roles` catalog table.
func verticaRoleFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any, role string) (exists bool, assigned []string, err error) {
	query := "select name, assigned_roles from roles where name ilike " + verticaQuoteLiteral(role)
	res, err := verticaVsql(ctx, conn, args, query)
	if err != nil {
		return false, nil, err
	}
	if res.RC != 0 {
		return false, nil, errArg("vertica_role: unable to read role facts: %s", strings.TrimSpace(res.Stderr))
	}
	rows := verticaParseRows(res.Stdout)
	if len(rows) == 0 {
		return false, nil, nil
	}
	row := rows[0]
	if len(row) < 2 {
		return true, nil, nil
	}
	return true, verticaParseCatalogList(row[1]), nil
}

// verticaUpdateRoleGrants revokes every role in existing not in
// required, then grants every role in required not in existing —
// matching real vertica_role's own update_roles.
func verticaUpdateRoleGrants(ctx context.Context, conn remoteexec.Connection, args map[string]any, role string, existing, required []string) error {
	for _, r := range existing {
		if !containsStr(required, r) {
			res, err := verticaVsql(ctx, conn, args, "revoke "+r+" from "+role)
			if err != nil {
				return err
			}
			if res.RC != 0 {
				return errArg("vertica_role: unable to revoke %s from %s: %s", r, role, strings.TrimSpace(res.Stderr))
			}
		}
	}
	for _, r := range required {
		if !containsStr(existing, r) {
			res, err := verticaVsql(ctx, conn, args, "grant "+r+" to "+role)
			if err != nil {
				return err
			}
			if res.RC != 0 {
				return errArg("vertica_role: unable to grant %s to %s: %s", r, role, strings.TrimSpace(res.Stderr))
			}
		}
	}
	return nil
}
