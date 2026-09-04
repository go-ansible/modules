package modules

import (
	"context"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// udmUserOptionalStrFields are udm_user's scalar options that carry no
// real default (real argument_spec default: null): sent only when the
// caller actually supplied a value, matching real udm_user.py's own
// `module.params[param_name] is not None` guard in its per-key
// obj[k]=... loop. key is both the primary (camelCase, UDM-native)
// argument name and the UDM attribute it is sent under; alias is the
// snake_case Ansible-style alternative spelling.
var udmUserOptionalStrFields = []udmShareStrField{
	{"birthday", "", ""},
	{"city", "", ""},
	{"country", "", ""},
	{"departmentNumber", "department_number", ""},
	{"description", "", ""},
	{"employeeNumber", "employee_number", ""},
	{"employeeType", "employee_type", ""},
	{"gecos", "", ""},
	{"homeShare", "home_share", ""},
	{"homeSharePath", "home_share_path", ""},
	{"homedrive", "", ""},
	{"mailHomeServer", "mail_home_server", ""},
	{"mailPrimaryAddress", "mail_primary_address", ""},
	{"organisation", "organization", ""},
	{"postcode", "", ""},
	{"primaryGroup", "primary_group", ""},
	{"profilepath", "", ""},
	{"pwdChangeNextLogin", "pwd_change_next_login", ""},
	{"roomNumber", "room_number", ""},
	{"sambahome", "", ""},
	{"scriptpath", "", ""},
	{"street", "", ""},
	{"title", "", ""},
}

// udmUserListFields are udm_user's list options, all of which default
// to [] (or [""] for email/serviceprovider) and so are always sent,
// matching real udm_user.py's own unconditional obj[k]=module.params[k]
// for these (list-typed argument_spec entries are never None).
var udmUserListFields = []udmShareListField{
	{"homeTelephoneNumber", "home_telephone_number"},
	{"mailAlternativeAddress", "mail_alternative_address"},
	{"mobileTelephoneNumber", "mobile_telephone_number"},
	{"pagerTelephonenumber", "pager_telephonenumber"},
	{"phone", ""},
	{"sambaPrivileges", "samba_privileges"},
	{"sambaUserWorkstations", "samba_user_workstations"},
	{"secretary", ""},
}

// moduleUdmUser implements Ansible's `udm_user` (community.general)
// module: manages a POSIX user on a Univention Corporate Server (UCS).
// See udmBin's own doc comment (udm_common.go) for this port's `udm`
// CLI substitution.
//
// Args: username (string, required, alias name); firstname/lastname/
// password (string, required when state=present, matching real
// udm_user.py's own required_if); state (present|absent, default
// "present"); position (string, default "") — full LDAP container DN,
// taking precedence over ou/subpath; ou (default ""); subpath (default
// "cn=users") — container built as "<subpath><ou><base_dn>" when
// position is empty, identical logic to udm_group.go's own
// udmGroupContainer; display_name (alias displayName) — defaults to
// "<firstname> <lastname>" when unset; unixhome — defaults to
// "/home/<username>" when unset; userexpiry — defaults to one year from
// today, but (a narrowing from real udm_user.py, which re-applies this
// default to any existing user whose stored userexpiry reads back as
// unset) this port only applies that default at CREATE time, since
// checking whether an already-existing UCS user's userexpiry is
// genuinely unset vs. merely not mentioned in this task would need an
// extra round trip this port does not make; email (list, default
// [""]) — sent under the UDM attribute name "e-mail" (not "email"),
// matching real udm_user.py's own special case; groups (list, default
// []) — handled as a separate post-create/modify step (see below), not
// a umc_module_for_edit attribute; every other option documented by
// real udm_user.py (birthday, city, country, department_number,
// description, employee_number/_type, gecos, home_share(_path),
// homedrive, mail_alternative_address, mail_home_server,
// mail_primary_address, mobile_telephone_number, organisation,
// pager_telephonenumber, phone, postcode, primary_group, profilepath,
// pwd_change_next_login, room_number, samba_privileges,
// samba_user_workstations, sambahome, scriptpath, secretary,
// serviceprovider, shell (default "/bin/bash"), street, title) is sent
// under its own UDM-native (camelCase where the real module defines
// one) attribute name — see udmUserOptionalStrFields/udmUserListFields
// above for the exact set, each accepting both its primary name and its
// snake_case alias.
//
// update_password (always|on_create, default "always"): real
// udm_user.py only rewrites an existing user's stored password when its
// existing hash fails to verify against the newly-supplied plaintext
// (via passlib's CryptContext) — this port has no local access to a
// crypt-hash verifier against a hash it never reads back over the `udm`
// CLI, so it cannot replicate that comparison. Instead: update_password
// = "on_create" NEVER touches password on an already-existing user
// (matching the real module's own outcome for a caller who intended
// "set it once"); update_password = "always" (the default)
// UNCONDITIONALLY re-sends password (and overridePWHistory/
// overridePWLength, if the caller set them) on every run against an
// existing user, which is a documented over-approximation: this port
// reports Changed and re-sets the password every run rather than only
// when it actually differs, whereas real udm_user.py is idempotent
// here. A caller relying on idempotent password handling should set
// update_password: on_create.
//
// groups: after the user object itself is created/reconciled, for each
// named group this port looks the group up by its own "name" attribute
// (unscoped, matching real udm_user.py's own LDAP OR-filter search) and,
// if the user's own DN is not already in that group's "users" attribute,
// runs `udm groups/group modify --dn <group-dn> --append
// users=<user-dn>` — matching real udm_user.py's own
// `grp["users"].append(user_dn); grp.modify()` exactly. Removing a user
// from a group by omitting it from a later run's groups list is NOT
// supported, matching real udm_user.py's own identical one-directional
// (add-only) behavior.
func moduleUdmUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	username := argString(args, "username", argString(args, "name", ""))
	if username == "" {
		return Result{}, errArg("udm_user: missing required argument: username (or its alias name)")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("udm_user: state must be present or absent, got %q", state)
	}

	container, err := udmUserContainer(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	userDN := "uid=" + username + "," + container
	modulePath := "users/user"
	scope := udmScope{Position: container}

	obj, err := udmFind(ctx, conn, modulePath, "username="+username, scope)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if obj == nil {
			return Ok(username + " already absent"), nil
		}
		if err := udmRemove(ctx, conn, modulePath, obj.DN); err != nil {
			return Result{}, err
		}
		return Changed(username + " removed").WithExtra("container", container), nil
	}

	firstname, err := requireString(args, "firstname")
	if err != nil {
		return Result{}, err
	}
	lastname, err := requireString(args, "lastname")
	if err != nil {
		return Result{}, err
	}
	password, err := requireString(args, "password")
	if err != nil {
		return Result{}, err
	}
	updatePassword := argString(args, "update_password", "always")

	displayName := argStringAliased(args, "displayName", "display_name", "")
	if displayName == "" {
		displayName = firstname + " " + lastname
	}
	unixhome := argString(args, "unixhome", "")
	if unixhome == "" {
		unixhome = "/home/" + username
	}
	shell := argString(args, "shell", "/bin/bash")
	email := argStringList(args, "email")
	if email == nil {
		email = []string{""}
	}
	serviceprovider := argStringList(args, "serviceprovider")
	if serviceprovider == nil {
		serviceprovider = []string{""}
	}

	desired := map[string][]string{
		"username":    {username},
		"firstname":   {firstname},
		"lastname":    {lastname},
		"displayName": {displayName},
		"unixhome":    {unixhome},
		"shell":       {shell},
		"e-mail":      email,
		"serviceprovider": serviceprovider,
	}
	for _, f := range udmUserOptionalStrFields {
		if v := argStringAliased(args, f.key, f.alias, ""); v != "" {
			desired[f.key] = []string{v}
		}
	}
	for _, f := range udmUserListFields {
		desired[f.key] = argStringListAliased(args, f.key, f.alias)
	}

	justCreated := false
	if obj == nil {
		justCreated = true
		desired["password"] = []string{password}
		if userexpiry := argString(args, "userexpiry", ""); userexpiry != "" {
			desired["userexpiry"] = []string{userexpiry}
		} else {
			desired["userexpiry"] = []string{time.Now().AddDate(1, 0, 0).Format("2006-01-02")}
		}
		if err := udmCreate(ctx, conn, modulePath, scope, desired); err != nil {
			return Result{}, err
		}
		obj = &udmObject{DN: userDN}
	} else {
		if updatePassword == "always" {
			desired["password"] = []string{password}
			if argBoolAliased(args, "overridePWHistory", "override_pw_history", false) {
				desired["overridePWHistory"] = []string{"1"}
			}
			if argBoolAliased(args, "overridePWLength", "override_pw_length", false) {
				desired["overridePWLength"] = []string{"1"}
			}
		}
		if userexpiry := argString(args, "userexpiry", ""); userexpiry != "" {
			desired["userexpiry"] = []string{userexpiry}
		}
	}

	changed := justCreated
	if !justCreated {
		c, err := udmReconcile(ctx, conn, modulePath, obj, desired)
		if err != nil {
			return Result{}, err
		}
		changed = c
	}

	groupsChanged, err := udmUserSyncGroups(ctx, conn, argStringList(args, "groups"), userDN)
	if err != nil {
		return Result{}, err
	}
	if groupsChanged {
		changed = true
	}

	if !changed {
		return Ok(username + " already up to date").WithExtra("container", container), nil
	}
	return Changed(username + " updated").WithExtra("container", container), nil
}

// udmUserContainer computes the user's LDAP container, matching real
// udm_user.py's own `container = position or (subpath + ou +
// base_dn())` logic exactly — identical shape to udm_group.go's own
// udmGroupContainer, but with subpath defaulting to "cn=users".
func udmUserContainer(ctx context.Context, conn remoteexec.Connection, args map[string]any) (string, error) {
	if position := argString(args, "position", ""); position != "" {
		return position, nil
	}
	baseDN, err := udmBaseDN(ctx, conn)
	if err != nil {
		return "", err
	}
	container := baseDN
	if ou := argString(args, "ou", ""); ou != "" {
		container = "ou=" + ou + "," + container
	}
	if subpath := argString(args, "subpath", "cn=users"); subpath != "" {
		container = subpath + "," + container
	}
	return container, nil
}

// udmUserSyncGroups adds userDN to every named group's own "users"
// attribute that doesn't already carry it — see moduleUdmUser's own doc
// comment for the fidelity this matches (and does not: it is add-only).
func udmUserSyncGroups(ctx context.Context, conn remoteexec.Connection, groups []string, userDN string) (bool, error) {
	changed := false
	for _, g := range groups {
		if g == "" {
			continue
		}
		grp, err := udmFind(ctx, conn, "groups/group", "name="+g, udmScope{})
		if err != nil {
			return changed, err
		}
		if grp == nil {
			continue
		}
		if containsString(grp.Attrs["users"], userDN) {
			continue
		}
		if err := udmAppend(ctx, conn, "groups/group", grp.DN, "users", userDN); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}
