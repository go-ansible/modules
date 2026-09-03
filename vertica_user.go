package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleVerticaUser implements Ansible's `vertica_user`
// (community.general) module: adds or removes a Vertica database user
// and, optionally, assigns roles to it.
//
// Deviation from real vertica_user: real vertica_user.py talks to
// Vertica through pyodbc; this port shells out to vsql instead — see
// vertica.go's own doc comments for the full rationale shared by every
// vertica_* module in this batch. A second, user-specific deviation:
// real vertica_user's own facts query joins `users` against
// `password_auditor` to read each user's current MD5 password hash
// (`p.password`) so it can compare it against the `password` argument
// and skip re-issuing `identified by` when unchanged; this port has no
// access to that catalog view's exact join semantics without a live
// cluster to verify column availability against (a hard rule of this
// batch is not to guess catalog shapes it cannot check), so it always
// re-applies `identified by` when a `password` argument is given,
// rather than risking a wrong idempotency check against a
// password/hash column this port cannot verify. Every other field
// (locked/expired/profile/resource_pool/roles) IS compared for
// idempotency, matching real vertica_user.
//
// Args: user (required, alias name); profile; resource_pool; password
// (the pre-hashed C("md5"+md5(password+username)) string real
// vertica_user itself documents — this port passes it through
// verbatim, it does not compute the hash); expired (bool); ldap
// (bool — creates/alters the user with password expired and
// identified by '$ldap$'); roles (string, comma separated, alias
// role); state (present|absent|locked, default present — "locked" is
// state=present plus an implied account-lock, matching real
// vertica_user's own `locked = (state == "locked")`); db, cluster
// (default localhost), port (default "5433"), login_user (default
// dbadmin), login_password.
func moduleVerticaUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	user := argString(args, "user", argString(args, "name", ""))
	if user == "" {
		return Result{}, errArg("vertica_user: user (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "locked" {
		return Result{}, errArg("vertica_user: state must be one of present, absent, locked, got %q", state)
	}
	locked := state == "locked"

	profile := strings.ToLower(argString(args, "profile", ""))
	resourcePool := strings.ToLower(argString(args, "resource_pool", ""))
	password := argString(args, "password", "")
	var expired, ldap *bool
	if _, ok := args["expired"]; ok {
		b := argBool(args, "expired", false)
		expired = &b
	}
	if _, ok := args["ldap"]; ok {
		b := argBool(args, "ldap", false)
		ldap = &b
	}
	roles := verticaSplitList(argString(args, "roles", argString(args, "role", "")))

	exists, facts, err := verticaUserFacts(ctx, conn, args, user)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(user + " already absent"), nil
		}
		if err := verticaUpdateRoleGrants(ctx, conn, args, user, facts.roles, nil); err != nil {
			return Result{}, err
		}
		res, err := verticaVsql(ctx, conn, args, "drop user "+user)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("vertica_user: dropping user failed (likely due to dependencies): " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(user + " dropped"), nil
	}

	// state == present or locked
	if !exists {
		frags := []string{"create user " + user}
		if locked {
			frags = append(frags, "account lock")
		}
		if password != "" {
			frags = append(frags, "identified by "+verticaQuoteLiteral(password))
		} else if ldap != nil && *ldap {
			frags = append(frags, "identified by "+verticaQuoteLiteral("$ldap$"))
		}
		if (expired != nil && *expired) || (ldap != nil && *ldap) {
			frags = append(frags, "password expire")
		}
		if profile != "" {
			frags = append(frags, "profile "+profile)
		}
		if resourcePool != "" {
			frags = append(frags, "resource pool "+resourcePool)
		}
		res, err := verticaVsql(ctx, conn, args, strings.Join(frags, " "))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("vertica_user: unable to create user " + user + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		if resourcePool != "" && resourcePool != "general" {
			if res, err := verticaVsql(ctx, conn, args, "grant usage on resource pool "+resourcePool+" to "+user); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("vertica_user: unable to grant resource pool usage: " + strings.TrimSpace(res.Stderr)), nil
			}
		}
		if err := verticaUpdateRoleGrants(ctx, conn, args, user, nil, roles); err != nil {
			return Result{}, err
		}
		return Changed(user + " created"), nil
	}

	changed := false
	frags := []string{"alter user " + user}
	if locked != facts.locked {
		if locked {
			frags = append(frags, "account lock")
		} else {
			frags = append(frags, "account unlock")
		}
		changed = true
	}
	if password != "" {
		frags = append(frags, "identified by "+verticaQuoteLiteral(password))
		changed = true
	}
	if ldap != nil && *ldap {
		if *ldap != facts.expired {
			frags = append(frags, "password expire")
			changed = true
		}
	} else if expired != nil && *expired != facts.expired {
		if *expired {
			frags = append(frags, "password expire")
			changed = true
		} else {
			return Fail("vertica_user: unexpiring a user's password is not supported"), nil
		}
	}
	if profile != "" && profile != facts.profile {
		frags = append(frags, "profile "+profile)
		changed = true
	}
	poolChanged := false
	if resourcePool != "" && resourcePool != facts.resourcePool {
		frags = append(frags, "resource pool "+resourcePool)
		poolChanged = true
		changed = true
	}
	if changed {
		if res, err := verticaVsql(ctx, conn, args, strings.Join(frags, " ")); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("vertica_user: unable to alter user " + user + ": " + strings.TrimSpace(res.Stderr)), nil
		}
	}
	if poolChanged {
		if facts.resourcePool != "" && facts.resourcePool != "general" {
			if res, err := verticaVsql(ctx, conn, args, "revoke usage on resource pool "+facts.resourcePool+" from "+user); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("vertica_user: unable to revoke resource pool usage: " + strings.TrimSpace(res.Stderr)), nil
			}
		}
		if resourcePool != "general" {
			if res, err := verticaVsql(ctx, conn, args, "grant usage on resource pool "+resourcePool+" to "+user); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("vertica_user: unable to grant resource pool usage: " + strings.TrimSpace(res.Stderr)), nil
			}
		}
	}
	if len(roles) > 0 && (!verticaSameSet(roles, facts.roles) || !verticaSameSet(roles, facts.defaultRoles)) {
		if err := verticaUpdateRoleGrants(ctx, conn, args, user, facts.roles, roles); err != nil {
			return Result{}, err
		}
		changed = true
	}
	if changed {
		return Changed(user + " updated"), nil
	}
	return Ok(user + " unchanged"), nil
}

type verticaUserRow struct {
	locked, expired       bool
	profile, resourcePool string
	roles, defaultRoles   []string
}

// verticaUserFacts reads a user's current state from the `users`
// catalog table, joined against `password_auditor` for its expiry
// state — matching real vertica_user's own get_user_facts, minus the
// password hash column itself (see moduleVerticaUser's own doc
// comment on why).
func verticaUserFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any, user string) (bool, verticaUserRow, error) {
	query := "select u.user_name, u.is_locked, p.acctexpired, u.profile_name, u.resource_pool, u.all_roles, u.default_roles " +
		"from users u join password_auditor p on p.user_id = u.user_id " +
		"where not u.is_super_user and u.user_name ilike " + verticaQuoteLiteral(user)
	res, err := verticaVsql(ctx, conn, args, query)
	if err != nil {
		return false, verticaUserRow{}, err
	}
	if res.RC != 0 {
		return false, verticaUserRow{}, errArg("vertica_user: unable to read user facts: %s", strings.TrimSpace(res.Stderr))
	}
	rows := verticaParseRows(res.Stdout)
	if len(rows) == 0 {
		return false, verticaUserRow{}, nil
	}
	row := rows[0]
	for len(row) < 7 {
		row = append(row, "")
	}
	return true, verticaUserRow{
		locked:       strings.EqualFold(row[1], "t") || strings.EqualFold(row[1], "true"),
		expired:      strings.EqualFold(row[2], "t") || strings.EqualFold(row[2], "true"),
		profile:      row[3],
		resourcePool: row[4],
		roles:        verticaParseCatalogList(row[5]),
		defaultRoles: verticaParseCatalogList(row[6]),
	}, nil
}
