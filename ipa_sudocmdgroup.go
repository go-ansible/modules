package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaSudocmdgroup implements Ansible's `ipa_sudocmdgroup` module:
// adds, modifies, and deletes FreeIPA sudo command groups, and manages
// their sudo-command membership, via the real `ipa` CLI's own
// `sudocmdgroup-add`/`sudocmdgroup-mod`/`sudocmdgroup-del`/
// `sudocmdgroup-add-member`/`sudocmdgroup-remove-member`/
// `sudocmdgroup-show` subcommands. See ipa_common.go's own doc comment
// for this port's shared architecture, including the connection-
// argument gap, the Kerberos-ticket precondition, and
// ipaReconcileMembers's own add/remove-set computation this module
// reuses for its sudocmd list.
//
// Args:
//   - cn (required, aliased name) — the group's pkey.
//   - description -> description -> `--description` (verified against
//     FreeIPA's own published API command reference,
//     freeipa.readthedocs.io/en/latest/api/sudocmdgroup_add.html).
//   - sudocmd (list of string) — sudo commands assigned to the group,
//     via `sudocmdgroup-add-member`/`-remove-member`'s own `--sudocmd`
//     flag (matching the same verified flag ipa_sudorule.go already
//     uses for allow/deny commands). Matching real ipa_sudocmdgroup's
//     own documented contract: an explicit empty list removes every
//     assigned command; omitting the arg entirely leaves membership
//     unchecked and unchanged.
//   - state (present|absent|enabled|disabled, default present).
//
// ⚠ state=enabled/disabled DELETES the sudo command group — the exact
// same real, verified quirk documented in ipa_sudocmd.go's own doc
// comment (real ipa_sudocmdgroup's own `ensure()` function has the
// identical `if state == "present": ...else: delete` shape, with
// "enabled"/"disabled" listed as argspec choices but never checked).
// FreeIPA sudo command groups have no enable/disable concept either.
// This port replicates the real, verified behavior exactly.
//
// Idempotency: `sudocmdgroup-show <cn> --all --raw` is parsed;
// description is diffed against it (ipaScalarDiff); sudocmd membership
// (when given) is reconciled against the group's own raw
// "member_sudocmd"-family attribute — verified from real
// ipa_sudocmdgroup's own source, which reads `ipa_sudocmdgroup.get(
// "member_sudocmd", [])` — via ipaReconcileMembers with pruneExtra=true
// (real ipa_sudocmdgroup has no `append` option for this list either,
// same as ipa_sudorule's own host/hostgroup/user/usergroup lists), only
// calling add-member/remove-member for the sets that actually differ.
//
// Deviation vs real ipa_sudocmdgroup: real ipa_sudocmdgroup returns the
// full post-change sudocmdgroup dict as its own `sudocmdgroup` return
// value (note: its own main() actually names the exit_json kwarg
// `sudorule`, not `sudocmdgroup`, despite its RETURN VALUES
// documentation header saying `sudocmdgroup` — verified from source: a
// real, copy-paste doc/code mismatch inside ipa_sudocmdgroup itself);
// this port does not surface that dict in Result.Extra at all — only
// changed/failed/msg, matching every other ipa_* module already shipped
// in this port.
func moduleIpaSudocmdgroup(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "cn", argString(args, "name", ""))
	if name == "" {
		return Result{}, errArg("ipa_sudocmdgroup: cn (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "enabled" && state != "disabled" {
		return Result{}, errArg("ipa_sudocmdgroup: state must be present, absent, enabled, or disabled, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_sudocmdgroup"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "sudocmdgroup", name)
	if err != nil {
		return Result{}, err
	}

	// See moduleIpaSudocmdgroup's own doc comment: only state=="present"
	// adds/modifies — every other value (including "enabled"/"disabled")
	// deletes, matching real ipa_sudocmdgroup exactly.
	if state != "present" {
		if !present {
			return Ok(name + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "sudocmdgroup-del", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudocmdgroup", "sudocmdgroup-del", res), nil
		}
		return Changed(name + " removed"), nil
	}

	changed := false

	if !present {
		flags := []string{"sudocmdgroup-add", name}
		if d := argString(args, "description", ""); d != "" {
			flags = append(flags, "--description="+d)
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudocmdgroup", "sudocmdgroup-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "sudocmdgroup", name)
		if err != nil {
			return Result{}, err
		}
	} else if flag, has := ipaScalarDiff(args, "description", "description", "description", current); has {
		res, err := ipaRun(ctx, conn, "sudocmdgroup-mod", name, flag)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudocmdgroup", "sudocmdgroup-mod", res), nil
		}
		changed = true
	}

	if _, ok := args["sudocmd"]; ok {
		desired := argStringList(args, "sudocmd")
		cur := current["member_sudocmd"]
		toAdd, toRemove := ipaReconcileMembers(cur, desired, true)
		if len(toAdd) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"sudocmdgroup-add-member", name}, ipaFlagRepeat("sudocmd", toAdd)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_sudocmdgroup", "sudocmdgroup-add-member", res), nil
			}
			changed = true
		}
		if len(toRemove) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"sudocmdgroup-remove-member", name}, ipaFlagRepeat("sudocmd", toRemove)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_sudocmdgroup", "sudocmdgroup-remove-member", res), nil
			}
			changed = true
		}
	}

	if !changed {
		return Ok(name + " already up to date"), nil
	}
	return Changed(name + " updated"), nil
}
