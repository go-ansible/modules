package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaHbacrule implements (a subset of) Ansible's `ipa_hbacrule`
// module: manages a FreeIPA HBAC (host-based access control) rule via
// the real `ipa` CLI's own `hbacrule-add`/`hbacrule-mod`/
// `hbacrule-del`/`hbacrule-add-host`/`hbacrule-add-user`/
// `hbacrule-add-service`/`hbacrule-add-sourcehost` (and their
// `-remove-` counterparts)/`hbacrule-show` subcommands. See
// ipa_common.go's own doc comment for this port's shared architecture
// and the connection-argument gap.
//
// Args: cn (string, required, aliased from name); description
// (string); hostcategory, usercategory, servicecategory,
// sourcehostcategory (all choices=[all] only, matching real
// ipa_hbacrule's own argspec) -> `--hostcat`/`--usercat`/
// `--servicecat` (individually verified against FreeIPA's own API
// command reference) and `--sourcehostcat` (inferred by the same
// "…cat" abbreviation pattern the other three verified flags share,
// not independently confirmed — flagged here per this batch's own
// honesty rule); host, hostgroup (added/removed via `hbacrule-
// add-host`'s own verified `--host`/`--hostgroup` flags); user,
// usergroup (via `hbacrule-add-user`'s own `--user`/`--group` flags —
// note usergroup maps to `--group`, matching the object-type-name
// convention every member command in this port follows); service,
// servicegroup (via `hbacrule-add-service`'s own verified `--hbacsvc`/
// `--hbacsvcgroup` flags — NOT `--service`/`--servicegroup`, a real
// naming trap this port's own research caught); sourcehost,
// sourcehostgroup (via `hbacrule-add-sourcehost`'s own verified
// `--host`/`--hostgroup` flags, a SEPARATE subcommand from plain host/
// hostgroup, verified to exist and take the identical flag names);
// state (present|absent|enabled|disabled, default "present") — enabled/
// disabled is applied via the verified `--ipaenabledflag=TRUE|FALSE`
// scalar flag on `hbacrule-mod`, not a separate enable/disable
// subcommand (simpler, and this one flag was independently confirmed).
//
// None of host/hostgroup/user/usergroup/service/servicegroup/
// sourcehost/sourcehostgroup has an `append` option in real
// ipa_hbacrule (unlike ipa_group/ipa_hostgroup) — every one given here
// is reconciled to EXACTLY the given list, EXCEPT service/servicegroup:
// FreeIPA's hbacsvc and hbacsvcgroup objects are BOTH named by a plain
// `cn=` RDN, so — unlike host (`fqdn=`) vs hostgroup (`cn=`), or user
// (`uid=`) vs usergroup (`cn=`), which this port's ipaMemberKind can
// tell apart from the DN alone — this port cannot distinguish a
// service member from a service-GROUP member by DN shape alone without
// also knowing their container path, which it could not verify against
// a live server. service/servicegroup are therefore handled ADD-ONLY
// (matching the same honest trade-off ipa_role.go's own privilege
// argument and ipa_service.go's own hosts argument make, for the same
// underlying reason).
//
// The raw attribute names this port reads for its idempotency pre-
// check on host/hostgroup ("memberhost"), user/usergroup
// ("memberuser"), and sourcehost/sourcehostgroup ("sourcehost")
// follow FreeIPA's well-established member-attribute naming
// convention but, unlike the add/remove CLI flags themselves, were
// not individually confirmed against a live server. If any of these
// three is wrong, the practical failure mode is a harmlessly
// redundant re-add (this port's pre-read would come back empty, so
// every listed member looks "new"; FreeIPA's own add-member is a safe
// no-op for an already-present member) rather than any incorrect
// removal, since a wrong attribute name simply yields an empty
// current-members read, never a wrong one to delete from.
func moduleIpaHbacrule(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cn := argString(args, "cn", argString(args, "name", ""))
	if cn == "" {
		return Result{}, errArg("ipa_hbacrule: cn (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "enabled" && state != "disabled" {
		return Result{}, errArg("ipa_hbacrule: state must be present, absent, enabled, or disabled, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	for _, cat := range []struct{ arg, listA, listB string }{
		{"hostcategory", "host", "hostgroup"},
		{"usercategory", "user", "usergroup"},
		{"servicecategory", "service", "servicegroup"},
		{"sourcehostcategory", "sourcehost", "sourcehostgroup"},
	} {
		v := argString(args, cat.arg, "")
		if v != "" && v != "all" {
			return Result{}, errArg("ipa_hbacrule: %s must be \"all\" if given, got %q", cat.arg, v)
		}
		if err := ipaCategoryConflict("ipa_hbacrule", cat.arg[:len(cat.arg)-8], v, len(argStringList(args, cat.listA)) > 0 || len(argStringList(args, cat.listB)) > 0); err != nil {
			return Result{}, err
		}
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_hbacrule"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "hbacrule", cn)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(cn + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "hbacrule-del", cn)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hbacrule", "hbacrule-del", res), nil
		}
		return Changed(cn + " removed"), nil
	}

	changed := false

	if !present {
		flags := []string{"hbacrule-add", cn}
		if d := argString(args, "description", ""); d != "" {
			flags = append(flags, "--description="+d)
		}
		flags = append(flags, ipaHbacCategoryFlags(args)...)
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hbacrule", "hbacrule-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "hbacrule", cn)
		if err != nil {
			return Result{}, err
		}
	} else {
		var modFlags []string
		if flag, has := ipaScalarDiff(args, "description", "description", "description", current); has {
			modFlags = append(modFlags, flag)
		}
		for _, spec := range ipaHbacCategorySpecs {
			if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flag)
			}
		}
		if len(modFlags) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"hbacrule-mod", cn}, modFlags...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_hbacrule", "hbacrule-mod", res), nil
			}
			changed = true
			current, _, err = ipaShow(ctx, conn, "hbacrule", cn)
			if err != nil {
				return Result{}, err
			}
		}
	}

	// host/hostgroup, user/usergroup, sourcehost/sourcehostgroup: each
	// pair shares one raw "member"-family attribute, classified by DN
	// shape (see ipa_common.go's own ipaMemberKind) into the two kinds.
	memberPairs := []ipaMemberPair{
		{"host", "host", "host", "hostgroup", "hostgroup", "cn", "memberhost", "hbacrule-add-host", "hbacrule-remove-host"},
		{"user", "user", "user", "usergroup", "group", "cn", "memberuser", "hbacrule-add-user", "hbacrule-remove-user"},
		{"sourcehost", "host", "host", "sourcehostgroup", "hostgroup", "cn", "sourcehost", "hbacrule-add-sourcehost", "hbacrule-remove-sourcehost"},
	}
	for _, p := range memberPairs {
		c, failRes, err := ipaReconcilePair(ctx, conn, "ipa_hbacrule", cn, current, args, p)
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

	// service/servicegroup: add-only — see moduleIpaHbacrule's own doc
	// comment on why (hbacsvc/hbacsvcgroup DNs can't be told apart by
	// RDN shape alone).
	if desired := argStringList(args, "service"); len(desired) > 0 {
		res, err := ipaRun(ctx, conn, append([]string{"hbacrule-add-service", cn}, ipaFlagRepeat("hbacsvc", desired)...)...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hbacrule", "hbacrule-add-service", res), nil
		}
		changed = true
	}
	if desired := argStringList(args, "servicegroup"); len(desired) > 0 {
		res, err := ipaRun(ctx, conn, append([]string{"hbacrule-add-service", cn}, ipaFlagRepeat("hbacsvcgroup", desired)...)...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hbacrule", "hbacrule-add-service (group)", res), nil
		}
		changed = true
	}

	locked := len(current["ipaenabledflag"]) > 0 && current["ipaenabledflag"][0] == "FALSE"
	if state == "disabled" && !locked {
		res, err := ipaRun(ctx, conn, "hbacrule-mod", cn, "--ipaenabledflag=FALSE")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hbacrule", "hbacrule-mod (disable)", res), nil
		}
		changed = true
	} else if state == "enabled" && locked {
		res, err := ipaRun(ctx, conn, "hbacrule-mod", cn, "--ipaenabledflag=TRUE")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_hbacrule", "hbacrule-mod (enable)", res), nil
		}
		changed = true
	}

	if !changed {
		return Ok(cn + " already up to date"), nil
	}
	return Changed(cn + " updated"), nil
}

var ipaHbacCategorySpecs = []ipaAttrSpec{
	{"hostcategory", "hostcat", "hostcategory"},
	{"usercategory", "usercat", "usercategory"},
	{"servicecategory", "servicecat", "servicecategory"},
	{"sourcehostcategory", "sourcehostcat", "sourcehostcategory"},
}

func ipaHbacCategoryFlags(args map[string]any) []string {
	var out []string
	for _, spec := range ipaHbacCategorySpecs {
		if v := argString(args, spec.arg, ""); v != "" {
			out = append(out, "--"+spec.flag+"="+v)
		}
	}
	return out
}

// ipaMemberPair and ipaReconcilePair (the generic add/remove-member
// reconciliation engine shared by ipa_hbacrule.go and ipa_sudorule.go)
// live in ipa_common.go.
