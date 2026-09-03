package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaRole implements (a subset of) Ansible's `ipa_role` module:
// manages a FreeIPA RBAC role via the real `ipa` CLI's own
// `role-add`/`role-mod`/`role-del`/`role-add-member`/
// `role-remove-member`/`role-add-privilege`/`role-show` subcommands.
// See ipa_common.go's own doc comment for this port's shared
// architecture and the connection-argument gap.
//
// Args: cn (string, required, aliased from name); description
// (string); user, group, host, hostgroup, service (list of string) —
// member principals of each kind, added/removed via `role-add-member`/
// `role-remove-member`'s own `--user`/`--group`/`--host`/`--hostgroup`/
// `--service` flags (verified); privilege (list of string) — added via
// the SEPARATE `role-add-privilege` subcommand's own `--privilege`
// flag (verified independently, since it is not part of
// role-add-member the way the others are); state (present|absent,
// default "present").
//
// Unlike ipa_group/ipa_hostgroup, real ipa_role has no `append` option
// at all — every member-list argument given here (user/group/host/
// hostgroup/service) is always reconciled to EXACTLY the given list
// (extra current members are removed), matching real ipa_role's own
// documented "if option is passed all assigned X that are not passed
// are unassigned" contract — EXCEPT privilege, which is a deliberate,
// documented exception: this port reads current members of the other
// five kinds off the role's own raw "member" attribute (see
// ipa_common.go's own ipaMemberKind), but a role's assigned privileges
// live on the PRIVILEGE side of the relationship (a reverse/indirect
// membership) under a raw attribute name this port could not verify
// without a live FreeIPA server to check against. Rather than guess at
// that attribute name and risk silently never detecting (and so never
// removing) a stale privilege — the kind of silently-wrong behavior
// this project's own house rules warn against — privilege is handled
// ADD-ONLY here: `role-add-privilege` is called with every privilege
// listed whenever the list is non-empty, and this always reports
// Changed=true for that call (this port cannot tell whether a listed
// privilege was already assigned) — the same honest trade-off
// ipa_group.go makes for its own external_user argument.
func moduleIpaRole(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cn := argString(args, "cn", argString(args, "name", ""))
	if cn == "" {
		return Result{}, errArg("ipa_role: cn (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_role: state must be present or absent, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_role"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "role", cn)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(cn + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "role-del", cn)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_role", "role-del", res), nil
		}
		return Changed(cn + " removed"), nil
	}

	changed := false

	if !present {
		flags := []string{"role-add", cn}
		if d := argString(args, "description", ""); d != "" {
			flags = append(flags, "--description="+d)
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_role", "role-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "role", cn)
		if err != nil {
			return Result{}, err
		}
	} else if flag, has := ipaScalarDiff(args, "description", "description", "description", current); has {
		res, err := ipaRun(ctx, conn, "role-mod", cn, flag)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_role", "role-mod", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "role", cn)
		if err != nil {
			return Result{}, err
		}
	}

	memberKinds := []struct{ arg, flag, kind string }{
		{"user", "user", "user"},
		{"group", "group", "cn"},
		{"host", "host", "host"},
		{"hostgroup", "hostgroup", "cn"},
		{"service", "service", "service"},
	}
	for _, mk := range memberKinds {
		if _, ok := args[mk.arg]; !ok {
			continue
		}
		desired := argStringList(args, mk.arg)
		cur := ipaCurrentMembersByKind(current, "member", mk.kind)
		toAdd, toRemove := ipaReconcileMembers(cur, desired, true)
		if len(toAdd) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"role-add-member", cn}, ipaFlagRepeat(mk.flag, toAdd)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_role", "role-add-member ("+mk.arg+")", res), nil
			}
			changed = true
		}
		if len(toRemove) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"role-remove-member", cn}, ipaFlagRepeat(mk.flag, toRemove)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_role", "role-remove-member ("+mk.arg+")", res), nil
			}
			changed = true
		}
	}

	// privilege is intentionally add-only — see moduleIpaRole's own doc
	// comment on why this port cannot safely compute a removal set for
	// it the way it does for user/group/host/hostgroup/service.
	if desired := argStringList(args, "privilege"); len(desired) > 0 {
		res, err := ipaRun(ctx, conn, append([]string{"role-add-privilege", cn}, ipaFlagRepeat("privilege", desired)...)...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_role", "role-add-privilege", res), nil
		}
		changed = true
	}

	if !changed {
		return Ok(cn + " already up to date"), nil
	}
	return Changed(cn + " updated"), nil
}
