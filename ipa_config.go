package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaConfig implements Ansible's `ipa_config` module: modifies
// FreeIPA's global configuration (the singleton "config" entry) via the
// real `ipa` CLI's own `config-show`/`config-mod` subcommands. See
// ipa_common.go's own doc comment for this port's shared architecture,
// including the documented connection-argument gap (ipa_host/ipa_port/
// ipa_prot/ipa_user/ipa_pass/ipa_timeout/validate_certs accepted, not
// wired into anything) and the "ipa CLI requires a pre-existing Kerberos
// ticket" precondition every ipa_* module inherits.
//
// Unlike every other ipa_* module in this port, ipa_config has no cn/
// name/state argument at all — matching real ipa_config's own argument
// spec, which has no `state` choice either: the global config entry
// always exists and is never created or deleted. Every arg below is
// optional and, when given, is diffed against `config-show --all --raw`
// and only sent via `config-mod` when it differs from the current
// value — a no-op run makes no `config-mod` call and reports unchanged,
// the same idempotency pattern as every other ipa_* module here.
//
// Args (all optional; verified against FreeIPA's own published API
// command reference, freeipa.readthedocs.io/en/latest/api/config_mod.html,
// per this batch's own hard rule — every flag below was individually
// confirmed to be exactly `--<param-name>`, identical to both its raw
// LDAP attribute name and its real ansible arg name; ipa_config is one
// of the few ipa_* modules where NO flag-name exception was found):
//   - ipaconfigstring (aliased configstring; list of string; choices
//     AllowNThash, "KDC:Disable Last Success", "KDC:Disable Lockout",
//     "KDC:Disable Default Preauth for SPNs") -> repeated --ipaconfigstring
//   - ipadefaultloginshell (aliased loginshell; scalar string)
//   - ipadefaultemaildomain (aliased emaildomain; scalar string)
//   - ipadefaultprimarygroup (aliased primarygroup; scalar string)
//   - ipagroupobjectclasses (aliased groupobjectclasses; list of string)
//     -> repeated --ipagroupobjectclasses
//   - ipagroupsearchfields (aliased groupsearchfields; list of string) —
//     real ipa_config joins this with "," into ONE string before sending
//     (verified from the real module's own source: `get_config_dict`
//     does `",".join(ipagroupsearchfields)`) — it is a single-valued LDAP
//     attribute despite its ansible arg type being a list, so this port
//     diffs/sends it as one comma-joined scalar flag, not a repeated one.
//   - ipahomesrootdir (aliased homesrootdir; scalar string)
//   - ipakrbauthzdata (aliased krbauthzdata; list of string; choices
//     MS-PAC, PAD, "nfs:NONE") -> repeated --ipakrbauthzdata
//   - ipamaxusernamelength (aliased maxusernamelength; int)
//   - ipapwdexpadvnotify (aliased pwdexpadvnotify; int)
//   - ipasearchrecordslimit (aliased searchrecordslimit; int)
//   - ipasearchtimelimit (aliased searchtimelimit; int)
//   - ipaselinuxusermaporder (aliased selinuxusermaporder; list of
//     string) — real ipa_config joins this with "$" into one string
//     before sending (verified from source: `"$".join(...)`); same
//     single-valued-attribute situation as ipagroupsearchfields above,
//     so this port diffs/sends it "$"-joined, not repeated.
//   - ipauserauthtype (aliased userauthtype; list of string; choices
//     password, radius, otp, pkinit, hardened, idp, passkey, disabled)
//     -> repeated --ipauserauthtype
//   - ipauserobjectclasses (aliased userobjectclasses; list of string)
//     -> repeated --ipauserobjectclasses
//   - ipausersearchfields (aliased usersearchfields; list of string) —
//     comma-joined scalar, same treatment as ipagroupsearchfields.
//
// Deviation vs real ipa_config: real ipa_config returns the full
// post-change config dict as its own `config` return value; matching
// every other ipa_* module already shipped in this port (ipa_user,
// ipa_sudorule, ipa_host, ...), this port does not surface that dict in
// Result.Extra — only changed/failed/msg.
func moduleIpaConfig(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	args = ipaWithAliases(args, ipaConfigAliases)

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_config"); !ok {
		return res, nil
	}

	res, err := ipaRun(ctx, conn, "config-show", "--all", "--raw")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return ipaFailedf("ipa_config", "config-show", res), nil
	}
	current := ipaParseRaw(res.Stdout)

	var modFlags []string
	for _, spec := range ipaConfigScalarSpecs {
		if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
			modFlags = append(modFlags, flag)
		}
	}
	for _, spec := range ipaConfigListSpecs {
		if flags, has := ipaListDiff(args, spec.arg, spec.flag, spec.raw, current); has {
			modFlags = append(modFlags, flags...)
		}
	}
	for _, spec := range ipaConfigJoinedSpecs {
		if flag, has := ipaJoinedScalarDiff(args, spec.arg, spec.flag, spec.raw, spec.sep, current); has {
			modFlags = append(modFlags, flag)
		}
	}

	if len(modFlags) == 0 {
		return Ok("ipa global configuration already up to date"), nil
	}

	res, err = ipaRun(ctx, conn, append([]string{"config-mod"}, modFlags...)...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return ipaFailedf("ipa_config", "config-mod", res), nil
	}
	return Changed("ipa global configuration updated"), nil
}

var ipaConfigAliases = [][2]string{
	{"ipaconfigstring", "configstring"},
	{"ipadefaultloginshell", "loginshell"},
	{"ipadefaultemaildomain", "emaildomain"},
	{"ipadefaultprimarygroup", "primarygroup"},
	{"ipagroupobjectclasses", "groupobjectclasses"},
	{"ipagroupsearchfields", "groupsearchfields"},
	{"ipahomesrootdir", "homesrootdir"},
	{"ipakrbauthzdata", "krbauthzdata"},
	{"ipamaxusernamelength", "maxusernamelength"},
	{"ipapwdexpadvnotify", "pwdexpadvnotify"},
	{"ipasearchrecordslimit", "searchrecordslimit"},
	{"ipasearchtimelimit", "searchtimelimit"},
	{"ipaselinuxusermaporder", "selinuxusermaporder"},
	{"ipauserauthtype", "userauthtype"},
	{"ipauserobjectclasses", "userobjectclasses"},
	{"ipausersearchfields", "usersearchfields"},
}

var ipaConfigScalarSpecs = []ipaAttrSpec{
	{"ipadefaultloginshell", "ipadefaultloginshell", "ipadefaultloginshell"},
	{"ipadefaultemaildomain", "ipadefaultemaildomain", "ipadefaultemaildomain"},
	{"ipadefaultprimarygroup", "ipadefaultprimarygroup", "ipadefaultprimarygroup"},
	{"ipahomesrootdir", "ipahomesrootdir", "ipahomesrootdir"},
	{"ipamaxusernamelength", "ipamaxusernamelength", "ipamaxusernamelength"},
	{"ipapwdexpadvnotify", "ipapwdexpadvnotify", "ipapwdexpadvnotify"},
	{"ipasearchrecordslimit", "ipasearchrecordslimit", "ipasearchrecordslimit"},
	{"ipasearchtimelimit", "ipasearchtimelimit", "ipasearchtimelimit"},
}

var ipaConfigListSpecs = []ipaAttrSpec{
	{"ipaconfigstring", "ipaconfigstring", "ipaconfigstring"},
	{"ipagroupobjectclasses", "ipagroupobjectclasses", "ipagroupobjectclasses"},
	{"ipakrbauthzdata", "ipakrbauthzdata", "ipakrbauthzdata"},
	{"ipauserauthtype", "ipauserauthtype", "ipauserauthtype"},
	{"ipauserobjectclasses", "ipauserobjectclasses", "ipauserobjectclasses"},
}

type ipaJoinedSpec struct {
	arg, flag, raw, sep string
}

var ipaConfigJoinedSpecs = []ipaJoinedSpec{
	{"ipagroupsearchfields", "ipagroupsearchfields", "ipagroupsearchfields", ","},
	{"ipausersearchfields", "ipausersearchfields", "ipausersearchfields", ","},
	{"ipaselinuxusermaporder", "ipaselinuxusermaporder", "ipaselinuxusermaporder", "$"},
}

// ipaWithAliases returns a copy of args where, for each (primary, alias)
// pair, args[primary] is populated from args[alias] whenever the primary
// key is absent — so every diff/create helper below only ever needs to
// look at the primary (canonical) key. Shared by every ipa_* module in
// this batch with more than one or two aliased args (ipa_user/
// ipa_sudorule only have one aliased arg each, so they inline the
// equivalent single-key fallback instead).
func ipaWithAliases(args map[string]any, pairs [][2]string) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	for _, p := range pairs {
		primary, alias := p[0], p[1]
		if _, ok := out[primary]; !ok {
			if v, ok := args[alias]; ok {
				out[primary] = v
			}
		}
	}
	return out
}

// ipaJoinedScalarDiff is ipaScalarDiff for an ansible list-of-string arg
// that real ipa_config (and friends) join into a SINGLE string attribute
// with sep before sending — see moduleIpaConfig's own doc comment for
// which fields these are and why.
func ipaJoinedScalarDiff(args map[string]any, argKey, flag, rawAttr, sep string, current map[string][]string) (string, bool) {
	if _, ok := args[argKey]; !ok {
		return "", false
	}
	want := strings.Join(argStringList(args, argKey), sep)
	have := ""
	if vals := current[rawAttr]; len(vals) > 0 {
		have = vals[0]
	}
	if want == have {
		return "", false
	}
	return "--" + flag + "=" + want, true
}
