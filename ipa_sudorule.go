package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaSudorule implements (a subset of) Ansible's `ipa_sudorule`
// module: manages a FreeIPA sudo rule via the real `ipa` CLI's own
// `sudorule-add`/`sudorule-mod`/`sudorule-del`/`sudorule-add-host`/
// `sudorule-add-user`/`sudorule-add-allow-command`/
// `sudorule-add-deny-command`/`sudorule-add-option`/`sudorule-show`
// subcommands (and their `-remove-`/`-remove-option` counterparts).
// See ipa_common.go's own doc comment for this port's shared
// architecture and the connection-argument gap.
//
// Args: cn (string, required, aliased from name); description
// (string); hostcategory, usercategory, cmdcategory (choices=[all]
// only) -> the verified `--hostcat`/`--usercat`/`--cmdcat` flags;
// runasusercategory, runasgroupcategory (choices=[all] only) ->
// `--runasusercat`/`--runasgroupcat`, inferred by the same "…cat"
// abbreviation pattern the other three share, not independently
// confirmed (flagged per this batch's own honesty rule, same tier as
// hbacrule's sourcehostcategory); host, hostgroup (via
// `sudorule-add-host`'s own verified `--host`/`--hostgroup` flags);
// user, usergroup (via `sudorule-add-user`'s own verified `--user`/
// `--group` flags); cmd, cmdgroup (via `sudorule-add-allow-command`'s
// own verified `--sudocmd`/`--sudocmdgroup` flags); deny_cmd,
// deny_cmdgroup (via the separate `sudorule-add-deny-command`
// subcommand, same verified `--sudocmd`/`--sudocmdgroup` flags);
// sudoopt (list of string) -> `sudorule-add-option`'s own verified
// `--ipasudoopt` flag (NOT `--sudooption`, a real naming trap this
// port's own research caught), one option string per call, reconciled
// like every other member-family list (add missing, remove extra) —
// unlike the DN-shaped attributes above, sudoopt's raw values are
// plain option strings, not DNs, so no DN classification is needed;
// state (present|absent|enabled|disabled, default "present") — enabled/
// disabled via the verified `--ipaenabledflag=TRUE|FALSE` flag on
// `sudorule-mod`, same as ipa_hbacrule.
//
// runasextusers (list of string, external RunAs users) is NOT
// implemented: real ipa_sudorule sends it as a plain JSON-RPC
// parameter, but this port could not find or verify a corresponding
// `ipa` CLI subcommand/flag for it (unlike every other member-list
// argument here, individually confirmed against FreeIPA's own API
// command reference) — rather than guess at an untested incantation,
// this arg is accepted (for argument-shape compatibility) but silently
// has no effect, and this is documented here rather than hidden.
// Real ipa_sudorule also has no runasuser/runasgroup list arguments at
// all (only the two RunAs *category* flags above plus this one
// external-users list), so there is nothing else RunAs-related in
// scope for this module.
//
// Host/hostgroup and user/usergroup have no `append` option in real
// ipa_sudorule (same as ipa_hbacrule) — both are reconciled to exactly
// the given list. cmd/cmdgroup and deny_cmd/deny_cmdgroup are
// similarly always reconciled exactly; the raw attribute names this
// port reads for their idempotency pre-check ("memberallowcmd"/
// "memberdenycmd", alongside "memberhost"/"memberuser" shared with
// ipa_hbacrule) follow the same well-established naming convention but
// were not individually confirmed — see ipa_hbacrule.go's own doc
// comment for the identical caveat and its fail-safe direction
// (a wrong name degrades to a harmless redundant re-add, never an
// incorrect removal).
func moduleIpaSudorule(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cn := argString(args, "cn", argString(args, "name", ""))
	if cn == "" {
		return Result{}, errArg("ipa_sudorule: cn (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "enabled" && state != "disabled" {
		return Result{}, errArg("ipa_sudorule: state must be present, absent, enabled, or disabled, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	for _, cat := range []struct{ arg, listA, listB string }{
		{"hostcategory", "host", "hostgroup"},
		{"usercategory", "user", "usergroup"},
		{"cmdcategory", "cmd", "cmdgroup"},
	} {
		v := argString(args, cat.arg, "")
		if v != "" && v != "all" {
			return Result{}, errArg("ipa_sudorule: %s must be \"all\" if given, got %q", cat.arg, v)
		}
		if err := ipaCategoryConflict("ipa_sudorule", cat.arg[:len(cat.arg)-8], v, len(argStringList(args, cat.listA)) > 0 || len(argStringList(args, cat.listB)) > 0); err != nil {
			return Result{}, err
		}
	}
	for _, arg := range []string{"runasusercategory", "runasgroupcategory"} {
		v := argString(args, arg, "")
		if v != "" && v != "all" {
			return Result{}, errArg("ipa_sudorule: %s must be \"all\" if given, got %q", arg, v)
		}
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_sudorule"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "sudorule", cn)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(cn + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "sudorule-del", cn)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudorule", "sudorule-del", res), nil
		}
		return Changed(cn + " removed"), nil
	}

	changed := false

	if !present {
		flags := []string{"sudorule-add", cn}
		if d := argString(args, "description", ""); d != "" {
			flags = append(flags, "--description="+d)
		}
		flags = append(flags, ipaSudoCategoryFlags(args)...)
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudorule", "sudorule-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "sudorule", cn)
		if err != nil {
			return Result{}, err
		}
	} else {
		var modFlags []string
		if flag, has := ipaScalarDiff(args, "description", "description", "description", current); has {
			modFlags = append(modFlags, flag)
		}
		for _, spec := range ipaSudoCategorySpecs {
			if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flag)
			}
		}
		if len(modFlags) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"sudorule-mod", cn}, modFlags...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_sudorule", "sudorule-mod", res), nil
			}
			changed = true
			current, _, err = ipaShow(ctx, conn, "sudorule", cn)
			if err != nil {
				return Result{}, err
			}
		}
	}

	memberPairs := []ipaMemberPair{
		{"host", "host", "host", "hostgroup", "hostgroup", "cn", "memberhost", "sudorule-add-host", "sudorule-remove-host"},
		{"user", "user", "user", "usergroup", "group", "cn", "memberuser", "sudorule-add-user", "sudorule-remove-user"},
		{"cmd", "sudocmd", "sudocmd", "cmdgroup", "sudocmdgroup", "cn", "memberallowcmd", "sudorule-add-allow-command", "sudorule-remove-allow-command"},
		{"deny_cmd", "sudocmd", "sudocmd", "deny_cmdgroup", "sudocmdgroup", "cn", "memberdenycmd", "sudorule-add-deny-command", "sudorule-remove-deny-command"},
	}
	for _, p := range memberPairs {
		c, failRes, err := ipaReconcilePair(ctx, conn, "ipa_sudorule", cn, current, args, p)
		if err != nil {
			return Result{}, err
		}
		if failRes.Failed {
			return failRes, nil
		}
		if c {
			changed = true
		}
	}

	if _, ok := args["sudoopt"]; ok {
		desired := argStringList(args, "sudoopt")
		cur := current["ipasudoopt"]
		toAdd, toRemove := ipaReconcileMembers(cur, desired, true)
		if len(toAdd) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"sudorule-add-option", cn}, ipaFlagRepeat("ipasudoopt", toAdd)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_sudorule", "sudorule-add-option", res), nil
			}
			changed = true
		}
		if len(toRemove) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"sudorule-remove-option", cn}, ipaFlagRepeat("ipasudoopt", toRemove)...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_sudorule", "sudorule-remove-option", res), nil
			}
			changed = true
		}
	}

	locked := len(current["ipaenabledflag"]) > 0 && current["ipaenabledflag"][0] == "FALSE"
	if state == "disabled" && !locked {
		res, err := ipaRun(ctx, conn, "sudorule-mod", cn, "--ipaenabledflag=FALSE")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudorule", "sudorule-mod (disable)", res), nil
		}
		changed = true
	} else if state == "enabled" && locked {
		res, err := ipaRun(ctx, conn, "sudorule-mod", cn, "--ipaenabledflag=TRUE")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_sudorule", "sudorule-mod (enable)", res), nil
		}
		changed = true
	}

	if !changed {
		return Ok(cn + " already up to date"), nil
	}
	return Changed(cn + " updated"), nil
}

var ipaSudoCategorySpecs = []ipaAttrSpec{
	{"hostcategory", "hostcat", "hostcategory"},
	{"usercategory", "usercat", "usercategory"},
	{"cmdcategory", "cmdcat", "cmdcategory"},
	{"runasusercategory", "runasusercat", "ipasudorunasusercategory"},
	{"runasgroupcategory", "runasgroupcat", "ipasudorunasgroupcategory"},
}

func ipaSudoCategoryFlags(args map[string]any) []string {
	var out []string
	for _, spec := range ipaSudoCategorySpecs {
		if v := argString(args, spec.arg, ""); v != "" {
			out = append(out, "--"+spec.flag+"="+v)
		}
	}
	return out
}
