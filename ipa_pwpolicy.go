package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaPwpolicy implements Ansible's `ipa_pwpolicy` module: adds,
// modifies, and deletes FreeIPA password policies via the real `ipa`
// CLI's own `pwpolicy-add`/`pwpolicy-mod`/`pwpolicy-del`/
// `pwpolicy-show` subcommands. See ipa_common.go's own doc comment for
// this port's shared architecture, including the connection-argument
// gap and the Kerberos-ticket precondition.
//
// A password policy's pkey is the group it applies to (cn); the GLOBAL
// policy (real ipa_pwpolicy's own "if group is omitted" case) is the
// group named "global_policy" — real ipa_pwpolicy's own
// PwPolicyIPAClient.pwpolicy_find hard-codes this exact name when
// `group` is None ("Manually set the cn to the global policy because
// pwpolicy_find will return a random different policy if cn is
// `None`"), and this port does the same.
//
// Args, and their ansible-name -> raw-LDAP-attribute-name -> `ipa` CLI
// flag mapping (raw names read directly from real ipa_pwpolicy's own
// source, plugins/modules/ipa_pwpolicy.py's `pwpolicy_options`/
// `pwpolicy_boolean_options` dicts and its own RETURN VALUES sample; CLI
// flags individually confirmed against FreeIPA's own published API
// command reference, freeipa.readthedocs.io/en/latest/api/pwpolicy_mod.html
// — pwpolicy is one of the ipa_* modules where the CLI flag is NOT
// simply `--<raw-attribute-name>`, unlike the majority documented in
// ipa_common.go's own doc comment):
//   - group (aliased name) — the policy's pkey; omitted means the
//     global policy (see above).
//   - maxpwdlife -> krbmaxpwdlife -> `--maxlife`
//   - minpwdlife -> krbminpwdlife -> `--minlife`
//   - historylength -> krbpwdhistorylength -> `--history`
//   - minclasses -> krbpwdmindiffchars -> `--minclasses`
//   - minlength -> krbpwdminlength -> `--minlength`
//   - priority -> cospriority -> `--priority` (required by real
//     ipa_pwpolicy when group is not the global policy; this port does
//     NOT enforce that constraint locally — an omitted priority on a
//     non-global policy create is passed straight through to
//     `pwpolicy-add`, which will itself reject it, surfaced via the
//     usual ipaFailedf path).
//   - maxfailcount -> krbpwdmaxfailure -> `--maxfail`
//   - failinterval -> krbpwdfailurecountinterval -> `--failinterval`
//   - lockouttime -> krbpwdlockoutduration -> `--lockouttime`
//   - gracelimit (int) -> passwordgracelimit -> `--gracelimit`
//   - maxrepeat (int) -> ipapwdmaxrepeat -> `--maxrepeat`
//   - maxsequence (int) -> ipapwdmaxsequence -> `--maxsequence`
//   - dictcheck (bool) -> ipapwddictcheck -> `--dictcheck=TRUE|FALSE`
//   - usercheck (bool) -> ipapwdusercheck -> `--usercheck=TRUE|FALSE`
//   - state (present|absent, default present).
//
// Idempotency: `pwpolicy-show <group> --all --raw` (or "global_policy"
// when group is omitted) is parsed and each given arg diffed against it
// before building one combined `pwpolicy-mod` call — a no-op run makes
// no `pwpolicy-mod` call at all and reports unchanged, same pattern as
// every other ipa_* module here.
//
// Deviation vs real ipa_pwpolicy: real ipa_pwpolicy returns the full
// post-change pwpolicy dict as its own `pwpolicy` return value; matching
// every other ipa_* module already shipped in this port, this port does
// not surface that dict in Result.Extra — only changed/failed/msg.
func moduleIpaPwpolicy(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	group := argString(args, "group", argString(args, "name", ""))
	pkey := group
	if pkey == "" {
		pkey = "global_policy"
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_pwpolicy: state must be present or absent, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_pwpolicy"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "pwpolicy", pkey)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(pkey + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "pwpolicy-del", pkey)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_pwpolicy", "pwpolicy-del", res), nil
		}
		return Changed(pkey + " removed"), nil
	}

	changed := false

	if !present {
		flags := []string{"pwpolicy-add", pkey}
		for _, spec := range ipaPwpolicyScalarSpecs {
			if v := argString(args, spec.arg, ""); v != "" {
				flags = append(flags, "--"+spec.flag+"="+v)
			}
		}
		for _, spec := range ipaPwpolicyBoolSpecs {
			if flag, has := ipaBoolFlag(args, spec.arg, spec.flag); has {
				flags = append(flags, flag)
			}
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_pwpolicy", "pwpolicy-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "pwpolicy", pkey)
		if err != nil {
			return Result{}, err
		}
	} else {
		var modFlags []string
		for _, spec := range ipaPwpolicyScalarSpecs {
			if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flag)
			}
		}
		for _, spec := range ipaPwpolicyBoolSpecs {
			if flag, has := ipaBoolDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flag)
			}
		}
		if len(modFlags) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"pwpolicy-mod", pkey}, modFlags...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_pwpolicy", "pwpolicy-mod", res), nil
			}
			changed = true
		}
	}

	if !changed {
		return Ok(pkey + " already up to date"), nil
	}
	return Changed(pkey + " updated"), nil
}

// ipaPwpolicyScalarSpecs: ansible arg -> `ipa` CLI flag -> raw LDAP
// attribute, verified against freeipa.readthedocs.io's own
// pwpolicy_mod.html — see moduleIpaPwpolicy's own doc comment.
var ipaPwpolicyScalarSpecs = []ipaAttrSpec{
	{"maxpwdlife", "maxlife", "krbmaxpwdlife"},
	{"minpwdlife", "minlife", "krbminpwdlife"},
	{"historylength", "history", "krbpwdhistorylength"},
	{"minclasses", "minclasses", "krbpwdmindiffchars"},
	{"minlength", "minlength", "krbpwdminlength"},
	{"priority", "priority", "cospriority"},
	{"maxfailcount", "maxfail", "krbpwdmaxfailure"},
	{"failinterval", "failinterval", "krbpwdfailurecountinterval"},
	{"lockouttime", "lockouttime", "krbpwdlockoutduration"},
	{"gracelimit", "gracelimit", "passwordgracelimit"},
	{"maxrepeat", "maxrepeat", "ipapwdmaxrepeat"},
	{"maxsequence", "maxsequence", "ipapwdmaxsequence"},
}

var ipaPwpolicyBoolSpecs = []ipaAttrSpec{
	{"dictcheck", "dictcheck", "ipapwddictcheck"},
	{"usercheck", "usercheck", "ipapwdusercheck"},
}
