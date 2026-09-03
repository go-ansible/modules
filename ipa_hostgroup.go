package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaHostgroup implements (a subset of) Ansible's `ipa_hostgroup`
// module: manages a FreeIPA host-group via the real `ipa` CLI's
// `hostgroup-add`/`hostgroup-mod`/`hostgroup-del`/
// `hostgroup-add-member`/`hostgroup-remove-member`/`hostgroup-show`
// subcommands. See ipa_common.go's own doc comment for this port's
// shared architecture and the connection-argument gap.
//
// Args: cn (string, required, aliased from name); description
// (string); host, hostgroup (list of string) — member hosts/nested
// host-groups, each independently optional; append (bool, default
// false) — false reconciles to exactly the given list (removing extra
// current members via `hostgroup-remove-member`), true only adds
// missing ones; state (present|absent|enabled|disabled, default
// "present") — matching real ipa_hostgroup's own documented "absent
// and disabled give the same results; present and enabled give the
// same results" exactly (this port normalizes enabled->present and
// disabled->absent up front, rather than guessing at a hostgroup-
// specific enable/disable subcommand that real ipa_hostgroup's own
// docs say doesn't behave any differently anyway).
//
// Member reconciliation reads the host-group's own raw "member"
// attribute and classifies each DN by its leading RDN (`fqdn=` for a
// host, `cn=` for a nested host-group) — see ipa_common.go's own
// ipaMemberKind doc comment.
func moduleIpaHostgroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cn := argString(args, "cn", argString(args, "name", ""))
	if cn == "" {
		return Result{}, errArg("ipa_hostgroup: cn (or name) is required")
	}
	state := argString(args, "state", "present")
	switch state {
	case "enabled":
		state = "present"
	case "disabled":
		state = "absent"
	case "present", "absent":
	default:
		return Result{}, errArg("ipa_hostgroup: state must be present, absent, enabled, or disabled, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	appendMode := argBool(args, "append", false)

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_hostgroup"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "hostgroup", cn)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(cn + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "hostgroup-del", cn)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hostgroup", "hostgroup-del", res), nil
		}
		return Changed(cn + " removed"), nil
	}

	changed := false

	if !present {
		flags := []string{"hostgroup-add", cn}
		if d := argString(args, "description", ""); d != "" {
			flags = append(flags, "--description="+d)
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hostgroup", "hostgroup-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "hostgroup", cn)
		if err != nil {
			return Result{}, err
		}
	} else if flag, has := ipaScalarDiff(args, "description", "description", "description", current); has {
		res, err := ipaRun(ctx, conn, "hostgroup-mod", cn, flag)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hostgroup", "hostgroup-mod", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "hostgroup", cn)
		if err != nil {
			return Result{}, err
		}
	}

	if _, ok := args["host"]; ok {
		desired := argStringList(args, "host")
		cur := ipaCurrentMembersByKind(current, "member", "host")
		toAdd, toRemove := ipaReconcileMembers(cur, desired, !appendMode)
		if len(toAdd) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"hostgroup-add-member", cn}, ipaFlagRepeat("host", toAdd)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_hostgroup", "hostgroup-add-member (host)", res), nil
			}
			changed = true
		}
		if len(toRemove) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"hostgroup-remove-member", cn}, ipaFlagRepeat("host", toRemove)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_hostgroup", "hostgroup-remove-member (host)", res), nil
			}
			changed = true
		}
	}

	if _, ok := args["hostgroup"]; ok {
		desired := argStringList(args, "hostgroup")
		cur := ipaCurrentMembersByKind(current, "member", "cn")
		toAdd, toRemove := ipaReconcileMembers(cur, desired, !appendMode)
		if len(toAdd) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"hostgroup-add-member", cn}, ipaFlagRepeat("hostgroup", toAdd)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_hostgroup", "hostgroup-add-member (hostgroup)", res), nil
			}
			changed = true
		}
		if len(toRemove) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"hostgroup-remove-member", cn}, ipaFlagRepeat("hostgroup", toRemove)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_hostgroup", "hostgroup-remove-member (hostgroup)", res), nil
			}
			changed = true
		}
	}

	if !changed {
		return Ok(cn + " already up to date"), nil
	}
	return Changed(cn + " updated"), nil
}
