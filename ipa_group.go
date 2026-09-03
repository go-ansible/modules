package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaGroup implements (a subset of) Ansible's `ipa_group`
// module: manages a FreeIPA POSIX (or non-POSIX/external) group via
// the real `ipa` CLI's `group-add`/`group-mod`/`group-del`/
// `group-add-member`/`group-remove-member`/`group-show` subcommands.
// See ipa_common.go's own doc comment for this port's shared
// architecture and the ipa_host/ipa_port/.../validate_certs
// connection-argument gap.
//
// Args: cn (string, required, aliased from name); description
// (string); gidnumber (string, aliased from gid); nonposix, external
// (bool, both create-time only — this port applies them on `group-add`
// but does not attempt to detect or toggle them on an already-existing
// group, since real FreeIPA itself treats POSIX-ness as effectively
// fixed at creation); state (present|absent, default "present"); user,
// group (list of string) — member users/groups, each independently
// optional (omitted = untouched, matching every real ipa_*
// member-list arg's own documented contract); append (bool, default
// false) — false means user/group are each reconciled to EXACTLY the
// given list (extra current members are removed via
// `group-remove-member`), true means only add missing members,
// leaving any others alone; external_user (list of string, requires
// external=true) — always (re)applied via `group-add-member
// --ipaexternalmember=...` when given and non-empty, and always
// reported as a change, exactly matching real ipa_group's own
// documented limitation ("unless SIDs are provided, the module always
// attempts to make changes even if the group already has all the
// users, because only SIDs are returned by IPA query") — this port
// doesn't pretend to do better.
//
// Member reconciliation reads the group's own raw "member" attribute
// (which holds both user- and group-member DNs together under one
// LDAP attribute) and classifies each by its leading RDN — see
// ipa_common.go's own ipaMemberKind doc comment.
func moduleIpaGroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cn := argString(args, "cn", argString(args, "name", ""))
	if cn == "" {
		return Result{}, errArg("ipa_group: cn (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_group: state must be present or absent, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	appendMode := argBool(args, "append", false)

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_group"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "group", cn)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(cn + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "group-del", cn)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_group", "group-del", res), nil
		}
		return Changed(cn + " removed"), nil
	}

	changed := false

	if !present {
		flags := []string{"group-add", cn}
		if d := argString(args, "description", ""); d != "" {
			flags = append(flags, "--description="+d)
		}
		if g := argString(args, "gidnumber", argString(args, "gid", "")); g != "" {
			flags = append(flags, "--gidnumber="+g)
		}
		if argBool(args, "nonposix", false) {
			flags = append(flags, "--nonposix")
		}
		if argBool(args, "external", false) {
			flags = append(flags, "--external")
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_group", "group-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "group", cn)
		if err != nil {
			return Result{}, err
		}
	} else {
		var modFlags []string
		if flag, has := ipaScalarDiff(args, "description", "description", "description", current); has {
			modFlags = append(modFlags, flag)
		}
		if flag, has := ipaScalarDiff(args, "gidnumber", "gidnumber", "gidnumber", current); has {
			modFlags = append(modFlags, flag)
		}
		if len(modFlags) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"group-mod", cn}, modFlags...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_group", "group-mod", res), nil
			}
			changed = true
			current, _, err = ipaShow(ctx, conn, "group", cn)
			if err != nil {
				return Result{}, err
			}
		}
	}

	if _, ok := args["user"]; ok {
		desired := argStringList(args, "user")
		cur := ipaCurrentMembersByKind(current, "member", "user")
		toAdd, toRemove := ipaReconcileMembers(cur, desired, !appendMode)
		if len(toAdd) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"group-add-member", cn}, ipaFlagRepeat("user", toAdd)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_group", "group-add-member (user)", res), nil
			}
			changed = true
		}
		if len(toRemove) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"group-remove-member", cn}, ipaFlagRepeat("user", toRemove)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_group", "group-remove-member (user)", res), nil
			}
			changed = true
		}
	}

	if _, ok := args["group"]; ok {
		desired := argStringList(args, "group")
		cur := ipaCurrentMembersByKind(current, "member", "cn")
		toAdd, toRemove := ipaReconcileMembers(cur, desired, !appendMode)
		if len(toAdd) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"group-add-member", cn}, ipaFlagRepeat("group", toAdd)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_group", "group-add-member (group)", res), nil
			}
			changed = true
		}
		if len(toRemove) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"group-remove-member", cn}, ipaFlagRepeat("group", toRemove)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_group", "group-remove-member (group)", res), nil
			}
			changed = true
		}
	}

	if extUsers := argStringList(args, "external_user"); len(extUsers) > 0 {
		res, err := ipaRun(ctx, conn, append([]string{"group-add-member", cn}, ipaFlagRepeat("ipaexternalmember", extUsers)...)...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_group", "group-add-member (external_user)", res), nil
		}
		changed = true
	}

	if !changed {
		return Ok(cn + " already up to date"), nil
	}
	return Changed(cn + " updated"), nil
}
