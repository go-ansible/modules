package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaSudocmd implements Ansible's `ipa_sudocmd` module: adds,
// modifies, and deletes FreeIPA sudo commands via the real `ipa` CLI's
// own `sudocmd-add`/`sudocmd-mod`/`sudocmd-del`/`sudocmd-show`
// subcommands. See ipa_common.go's own doc comment for this port's
// shared architecture, including the connection-argument gap and the
// Kerberos-ticket precondition.
//
// Args:
//   - sudocmd (required, aliased name) — the command's pkey.
//   - description -> description -> `--description` (verified against
//     FreeIPA's own published API command reference,
//     freeipa.readthedocs.io/en/latest/api/sudocmd_add.html).
//   - state (present|absent|enabled|disabled, default present).
//
// ⚠ state=enabled/disabled DELETES the sudo command — a real, verified
// quirk of real ipa_sudocmd, not a bug in this port. Real ipa_sudocmd's
// own `ensure()` function (plugins/modules/ipa_sudocmd.py) only ever
// branches on `if state == "present": ...else: delete` — despite its
// own argument spec listing "enabled"/"disabled" as valid `state`
// choices, NEITHER of those values is ever checked anywhere in
// `ensure()`; any state other than the literal string "present" falls
// through to the same `else` branch as "absent" and deletes the
// command. FreeIPA sudo commands have no enable/disable concept at all
// (only sudo RULES do, via ipa_sudorule's own state=enabled/disabled,
// which correctly maps to `--ipaenabledflag`) — this appears to be
// vestigial copy-paste from a sudorule-shaped template that was never
// wired up for sudocmd. This port replicates the real, verified
// behavior exactly (functional parity, quirks included, per this
// batch's own charter) rather than "fixing" it, since a caller relying
// on real ipa_sudocmd's documented choices would be surprised in the
// SAME way against a real FreeIPA server.
//
// Idempotency: `sudocmd-show <sudocmd> --all --raw` is parsed and
// description diffed against it (ipaScalarDiff) before building one
// `sudocmd-mod` call — a no-op run makes no `sudocmd-mod` call at all
// and reports unchanged.
//
// Deviation vs real ipa_sudocmd: real ipa_sudocmd returns the full
// post-change sudocmd dict as its own `sudocmd` return value; matching
// every other ipa_* module already shipped in this port, this port does
// not surface that dict in Result.Extra — only changed/failed/msg.
func moduleIpaSudocmd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "sudocmd", argString(args, "name", ""))
	if name == "" {
		return Result{}, errArg("ipa_sudocmd: sudocmd (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "enabled" && state != "disabled" {
		return Result{}, errArg("ipa_sudocmd: state must be present, absent, enabled, or disabled, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_sudocmd"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "sudocmd", name)
	if err != nil {
		return Result{}, err
	}

	// See moduleIpaSudocmd's own doc comment: state=="present" is the
	// ONLY value that adds/modifies — every other value (including
	// "enabled"/"disabled") deletes, matching real ipa_sudocmd exactly.
	if state != "present" {
		if !present {
			return Ok(name + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "sudocmd-del", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudocmd", "sudocmd-del", res), nil
		}
		return Changed(name + " removed"), nil
	}

	if !present {
		flags := []string{"sudocmd-add", name}
		if d := argString(args, "description", ""); d != "" {
			flags = append(flags, "--description="+d)
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudocmd", "sudocmd-add", res), nil
		}
		return Changed(name + " created"), nil
	}

	if flag, has := ipaScalarDiff(args, "description", "description", "description", current); has {
		res, err := ipaRun(ctx, conn, "sudocmd-mod", name, flag)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudocmd", "sudocmd-mod", res), nil
		}
		return Changed(name + " updated"), nil
	}

	return Ok(name + " already up to date"), nil
}
