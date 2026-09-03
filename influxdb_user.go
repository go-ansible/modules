package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleInfluxdbUser implements Ansible's `influxdb_user`
// (community.general) module: creates, updates, or drops an InfluxDB
// user, its admin role, and its per-database grants — read from real
// influxdb_user.py's own find_user/check_user_password/
// set_user_password/create_user/drop_user/set_user_grants functions
// (this batch's hard rule: the password-verification-by-switching-
// user shape and the grants set-diff are only visible there, not
// EXAMPLES/OPTIONS).
//
// Args: user_name (required); user_password; admin (bool, default
// false); state (present|absent, default present); grants ([]map with
// "database"/"privilege" keys) — if omitted (the key absent from
// args), existing grants are left alone entirely; if present (even an
// empty list), grants are reconciled to exactly match: any existing
// grant not in the desired list is revoked, any desired grant not
// already present is granted.
//
// present = user_name appears in `SHOW USERS`. state=absent drops the
// user if present, otherwise a no-op.
//
// state=present, user exists: this port verifies user_password (if
// given) by re-running `SHOW USERS` authenticated AS user_name with
// user_password rather than the module's own admin login credentials
// — a non-zero exit is treated as "password does not match", matching
// the true/false outcome of real check_user_password's own
// switch_user-then-probe-then-switch-back dance, though not its exact
// mechanism (see influxExecute's own doc comment on this port having
// no InfluxDB client library to call switch_user on directly). A
// mismatch (given a non-empty user_password) issues
// `SET PASSWORD FOR "user" = 'pw'`. admin is then reconciled via
// `GRANT ALL PRIVILEGES TO "user"` / `REVOKE ALL PRIVILEGES FROM
// "user"` if it differs from the user's current admin bit.
//
// state=present, user absent: `CREATE USER "user" WITH PASSWORD 'pw'`
// (pw defaults to the empty string, matching real influxdb_user's own
// `user_password = user_password or ""`), `WITH ALL PRIVILEGES`
// appended if admin=true.
//
// grants reconciliation (both branches, when grants is given): reads
// `SHOW GRANTS FOR "user"`, drops any row whose privilege is
// "NO PRIVILEGES", normalizes "ALL PRIVILEGES" to "ALL" (matching
// real set_user_grants's own parsed_grants transform), then diffs
// against the desired list by (database, privilege) equality —
// `REVOKE <priv> ON "db" FROM "user"` for each current grant not
// desired, `GRANT <priv> ON "db" TO "user"` for each desired grant not
// already current.
func moduleInfluxdbUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	userName, err := requireString(args, "user_name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("influxdb_user: state must be present or absent, got %q", state)
	}
	userPassword := argString(args, "user_password", "")
	admin := argBool(args, "admin", false)

	isAdmin, exists, err := influxFindUser(ctx, conn, args, userName)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(""), nil
		}
		return influxRunChange(ctx, conn, args, "influxdb_user: drop failed", "DROP USER "+influxIdent(userName))
	}

	changed := false
	if exists {
		if userPassword != "" {
			ok, err := influxCheckUserPassword(ctx, conn, args, userName, userPassword)
			if err != nil {
				return Result{}, err
			}
			if !ok {
				if _, err := influxRunChange(ctx, conn, args, "influxdb_user: set password failed",
					"SET PASSWORD FOR "+influxIdent(userName)+" = "+influxLiteral(userPassword)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}
		if admin && !isAdmin {
			if _, err := influxRunChange(ctx, conn, args, "influxdb_user: grant admin failed", "GRANT ALL PRIVILEGES TO "+influxIdent(userName)); err != nil {
				return Result{}, err
			}
			changed = true
		} else if !admin && isAdmin {
			if _, err := influxRunChange(ctx, conn, args, "influxdb_user: revoke admin failed", "REVOKE ALL PRIVILEGES FROM "+influxIdent(userName)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	} else {
		q := "CREATE USER " + influxIdent(userName) + " WITH PASSWORD " + influxLiteral(userPassword)
		if admin {
			q += " WITH ALL PRIVILEGES"
		}
		if _, err := influxRunChange(ctx, conn, args, "influxdb_user: create failed", q); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if rawGrants, ok := args["grants"]; ok {
		grantsChanged, err := influxReconcileGrants(ctx, conn, args, userName, rawGrants)
		if err != nil {
			return Result{}, err
		}
		if grantsChanged {
			changed = true
		}
	}

	if changed {
		return Changed(""), nil
	}
	return Ok(""), nil
}

// influxLiteral single-quotes s as an InfluxQL string literal,
// doubling any embedded single quote.
func influxLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func influxFindUser(ctx context.Context, conn remoteexec.Connection, args map[string]any, userName string) (isAdmin, exists bool, err error) {
	res, err := influxExecute(ctx, conn, args, "", "SHOW USERS")
	if err != nil {
		return false, false, err
	}
	if res.RC != 0 {
		return false, false, fmt.Errorf("influxdb_user: unable to list users: %s", strings.TrimSpace(res.Stderr))
	}
	rows, err := influxRows(res.Stdout)
	if err != nil {
		return false, false, fmt.Errorf("influxdb_user: %w", err)
	}
	for _, row := range rows {
		if fmt.Sprint(row["user"]) == userName {
			return influxToBool(row["admin"]), true, nil
		}
	}
	return false, false, nil
}

// influxCheckUserPassword matches real check_user_password's own
// boolean outcome — see moduleInfluxdbUser's own doc comment for how
// this port approximates its switch_user-based mechanism.
func influxCheckUserPassword(ctx context.Context, conn remoteexec.Connection, args map[string]any, userName, userPassword string) (bool, error) {
	probeArgs := make(map[string]any, len(args)+2)
	for k, v := range args {
		probeArgs[k] = v
	}
	probeArgs["username"] = userName
	delete(probeArgs, "login_username")
	probeArgs["password"] = userPassword
	delete(probeArgs, "login_password")
	res, err := influxExecute(ctx, conn, probeArgs, "", "SHOW USERS")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// influxReconcileGrants matches real set_user_grants exactly — see
// moduleInfluxdbUser's own doc comment.
func influxReconcileGrants(ctx context.Context, conn remoteexec.Connection, args map[string]any, userName string, rawGrants any) (bool, error) {
	desired, err := influxParseGrants(rawGrants)
	if err != nil {
		return false, err
	}

	res, err := influxExecute(ctx, conn, args, "", "SHOW GRANTS FOR "+influxIdent(userName))
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("influxdb_user: unable to list grants for %s: %s", userName, strings.TrimSpace(res.Stderr))
	}
	rows, err := influxRows(res.Stdout)
	if err != nil {
		return false, fmt.Errorf("influxdb_user: %w", err)
	}

	var current []influxGrant
	for _, row := range rows {
		priv := fmt.Sprint(row["privilege"])
		if priv == "NO PRIVILEGES" {
			continue
		}
		if priv == "ALL PRIVILEGES" {
			priv = "ALL"
		}
		current = append(current, influxGrant{database: fmt.Sprint(row["database"]), privilege: priv})
	}

	changed := false
	for _, cur := range current {
		if !influxGrantIn(cur, desired) {
			if _, err := influxRunChange(ctx, conn, args, "influxdb_user: revoke failed",
				"REVOKE "+cur.privilege+" ON "+influxIdent(cur.database)+" FROM "+influxIdent(userName)); err != nil {
				return false, err
			}
			changed = true
		}
	}
	for _, want := range desired {
		if !influxGrantIn(want, current) {
			if _, err := influxRunChange(ctx, conn, args, "influxdb_user: grant failed",
				"GRANT "+want.privilege+" ON "+influxIdent(want.database)+" TO "+influxIdent(userName)); err != nil {
				return false, err
			}
			changed = true
		}
	}
	return changed, nil
}

type influxGrant struct {
	database  string
	privilege string
}

func influxGrantIn(g influxGrant, list []influxGrant) bool {
	for _, item := range list {
		if item == g {
			return true
		}
	}
	return false
}

func influxParseGrants(raw any) ([]influxGrant, error) {
	list, ok := raw.([]any)
	if !ok {
		if raw == nil {
			return nil, nil
		}
		return nil, errArg("influxdb_user: grants must be a list")
	}
	grants := make([]influxGrant, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, errArg("influxdb_user: grants[%d] must be a dict", i)
		}
		db, err := requireString(m, "database")
		if err != nil {
			return nil, errArg("influxdb_user: grants[%d].database is required", i)
		}
		priv, err := requireString(m, "privilege")
		if err != nil {
			return nil, errArg("influxdb_user: grants[%d].privilege is required", i)
		}
		grants = append(grants, influxGrant{database: db, privilege: priv})
	}
	return grants, nil
}
