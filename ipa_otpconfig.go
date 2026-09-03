package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaOtpconfig implements Ansible's `ipa_otpconfig` module:
// modifies FreeIPA's global OTP (One Time Password) configuration —
// another singleton entry, like ipa_config's own "config" entry — via
// the real `ipa` CLI's own `otpconfig-show`/`otpconfig-mod` subcommands.
// See ipa_common.go's own doc comment for this port's shared
// architecture, including the connection-argument gap and the
// Kerberos-ticket precondition. See moduleIpaConfig's own doc comment
// for the identical "no cn/name/state argument at all" shape this
// module shares with it (the global OTP config entry always exists and
// is never created or deleted).
//
// Args (all optional int; verified against FreeIPA's own published API
// command reference, freeipa.readthedocs.io/en/latest/api/otpconfig_mod.html,
// per this batch's own hard rule — every flag below was individually
// confirmed to be exactly `--<param-name>`, identical to its raw LDAP
// attribute name and its real ansible arg name, same as ipa_config):
//   - ipatokentotpauthwindow (aliased totpauthwindow) — TOTP
//     authentication window in seconds.
//   - ipatokentotpsyncwindow (aliased totpsyncwindow) — TOTP
//     synchronization window in seconds.
//   - ipatokenhotpauthwindow (aliased hotpauthwindow) — HOTP
//     authentication window in number of hops.
//   - ipatokenhotpsyncwindow (aliased hotpsyncwindow) — HOTP
//     synchronization window in hops.
//
// Idempotency: `otpconfig-show --all --raw` is parsed and each given arg
// diffed against it (ipaScalarDiff) before building one combined
// `otpconfig-mod` call — a no-op run (nothing given differs) makes no
// `otpconfig-mod` call at all and reports unchanged.
//
// Deviation vs real ipa_otpconfig: real ipa_otpconfig returns the full
// post-change otpconfig dict as its own `otpconfig` return value;
// matching every other ipa_* module already shipped in this port, this
// port does not surface that dict in Result.Extra — only
// changed/failed/msg.
func moduleIpaOtpconfig(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	args = ipaWithAliases(args, ipaOtpconfigAliases)

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_otpconfig"); !ok {
		return res, nil
	}

	res, err := ipaRun(ctx, conn, "otpconfig-show", "--all", "--raw")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return ipaFailedf("ipa_otpconfig", "otpconfig-show", res), nil
	}
	current := ipaParseRaw(res.Stdout)

	var modFlags []string
	for _, spec := range ipaOtpconfigSpecs {
		if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
			modFlags = append(modFlags, flag)
		}
	}

	if len(modFlags) == 0 {
		return Ok("ipa OTP configuration already up to date"), nil
	}

	res, err = ipaRun(ctx, conn, append([]string{"otpconfig-mod"}, modFlags...)...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return ipaFailedf("ipa_otpconfig", "otpconfig-mod", res), nil
	}
	return Changed("ipa OTP configuration updated"), nil
}

var ipaOtpconfigAliases = [][2]string{
	{"ipatokentotpauthwindow", "totpauthwindow"},
	{"ipatokentotpsyncwindow", "totpsyncwindow"},
	{"ipatokenhotpauthwindow", "hotpauthwindow"},
	{"ipatokenhotpsyncwindow", "hotpsyncwindow"},
}

var ipaOtpconfigSpecs = []ipaAttrSpec{
	{"ipatokentotpauthwindow", "ipatokentotpauthwindow", "ipatokentotpauthwindow"},
	{"ipatokentotpsyncwindow", "ipatokentotpsyncwindow", "ipatokentotpsyncwindow"},
	{"ipatokenhotpauthwindow", "ipatokenhotpauthwindow", "ipatokenhotpauthwindow"},
	{"ipatokenhotpsyncwindow", "ipatokenhotpsyncwindow", "ipatokenhotpsyncwindow"},
}
