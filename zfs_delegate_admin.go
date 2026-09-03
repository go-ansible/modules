package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleZfsDelegateAdmin implements (a subset of) Ansible's
// `zfs_delegate_admin` module: grants or revokes ZFS delegated
// administration permissions via `zfs allow`/`zfs unallow`.
//
// Args: name (string, required) — a filesystem or volume name, e.g.
// "rpool/myfs"; state (present|absent, default "present"); users
// ([]string), groups ([]string), everyone (bool, default false) — the
// entities to grant/revoke permissions for; permissions ([]string,
// required if state=present) — the permission names to delegate (see
// `zfs allow` in zfs(1M)); local (bool, default false), descendents
// (bool, default false) — together select the `-l`/`-d`/`-ld` scope
// flag exactly like real zfs_delegate_admin: both or neither set means
// "-ld" (local+descendent, the default), local alone means "-l",
// descendents alone means "-d"; recursive (bool, default false) —
// `-r`, only meaningful for state=absent (`zfs unallow -r`).
//
// state=present requires at least one of users/groups/everyone, and
// runs one `zfs allow -<scope> -u user1,user2 permissions... name`
// call for users (if any), one `-g group1,group2` call for groups (if
// any), and one `-e permissions... name` call for everyone (if set) —
// matching real zfs_delegate_admin's own one-call-per-entity-type
// shape exactly. state=absent behaves the same way with `zfs unallow`
// (permissions omitted from the command entirely revokes ALL of that
// entity's permissions in that scope, matching real `zfs unallow`
// semantics when no permission list is given) — EXCEPT that at least
// one of users/groups/everyone is required here too.
//
// Deviation vs real zfs_delegate_admin: real zfs_delegate_admin
// supports state=absent with NONE of users/groups/everyone given, a
// "clear every permission" mode that first parses `zfs allow name`'s
// own free-text permissions table (a "Local permissions:"/"Descendent
// permissions:"/"Local+Descendent permissions:" section format) to
// discover every currently-delegated user/group/everyone entry, then
// issues one `zfs unallow` call per discovered entity. This port does
// not implement that text-table parse, and REJECTS state=absent with
// no entity arguments (errArg) rather than silently doing nothing or
// faking a clear; a caller wanting to revoke everything must name the
// users/groups/everyone explicitly. For the same reason, this port
// also cannot replicate real zfs_delegate_admin's own `changed`
// detection, which re-parses that same table before and after to see
// whether anything actually moved; every present/absent call that
// runs at least one `zfs allow`/`zfs unallow` here always reports
// Changed=true (a no-op grant/revoke still exits 0), matching this
// batch's house "can't cheaply tell a no-op apart" convention (see
// apt.go's own doc comment).
func moduleZfsDelegateAdmin(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("zfs_delegate_admin: state must be present or absent, got %q", state)
	}
	users := argStringList(args, "users")
	groups := argStringList(args, "groups")
	everyone := argBool(args, "everyone", false)
	permissions := argStringList(args, "permissions")

	if len(users) == 0 && len(groups) == 0 && !everyone {
		if state == "present" {
			return Result{}, errArg("zfs_delegate_admin: one of users, groups, or everyone must be set")
		}
		return Result{}, errArg("zfs_delegate_admin: this port does not implement state=absent with no entity given " +
			"(real zfs_delegate_admin parses `zfs allow`'s own free-text permissions table to clear every delegated " +
			"permission in that case; specify users, groups, or everyone explicitly instead)")
	}
	if state == "present" && len(permissions) == 0 {
		return Result{}, errArg("zfs_delegate_admin: permissions is required when state=present")
	}

	local := argBool(args, "local", false)
	descendents := argBool(args, "descendents", false)
	var scope string
	switch {
	case local && descendents, !local && !descendents:
		scope = "ld"
	case local:
		scope = "l"
	default:
		scope = "d"
	}

	subcommand := "allow"
	var prefix string
	if state == "absent" {
		subcommand = "unallow"
		if argBool(args, "recursive", false) {
			prefix = "-r "
		}
	}

	permSuffix := ""
	if len(permissions) > 0 {
		permSuffix = " " + shellQuote(strings.Join(permissions, ","))
	}

	var ran bool
	run1 := func(flag string) error {
		cmd := "zfs " + subcommand + " " + prefix + "-" + scope + " " + flag + permSuffix + " " + shellQuote(name)
		if _, err := run(ctx, conn, cmd); err != nil {
			return err
		}
		ran = true
		return nil
	}

	if len(users) > 0 {
		if err := run1("-u " + shellQuote(strings.Join(users, ","))); err != nil {
			return Result{}, err
		}
	}
	if len(groups) > 0 {
		if err := run1("-g " + shellQuote(strings.Join(groups, ","))); err != nil {
			return Result{}, err
		}
	}
	if everyone {
		if err := run1("-e"); err != nil {
			return Result{}, err
		}
	}

	if !ran {
		return Ok("nothing to do"), nil
	}
	return Changed("ZFS delegated admin permissions updated"), nil
}
