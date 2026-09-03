package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaUser implements (a subset of) Ansible's `ipa_user` module:
// manages a FreeIPA user account by shelling out to the real `ipa`
// CLI's own `user-add`/`user-mod`/`user-del`/`user-enable`/
// `user-disable`/`user-show` subcommands. See ipa_common.go's own doc
// comment for this port's shared architecture, including the
// documented gap around ipa_host/ipa_port/ipa_prot/ipa_user/ipa_pass/
// ipa_timeout/validate_certs (accepted for compatibility, not wired
// into anything — a Kerberos ticket on the target is the real
// precondition) and the verified `ipa` CLI flag-name mapping.
//
// Args: uid (string, required, aliased from name — the login name);
// state (present|absent|enabled|disabled, default "present");
// givenname, sn (both required by real ipa_user when creating a new
// user, matching real ipa_user's own documented constraint — this
// port enforces it in Go before ever invoking `ipa`); displayname,
// gidnumber, uidnumber, homedirectory, loginshell, title,
// krbpasswordexpiration (all scalar strings, mapped to the identically
// -named `ipa` CLI flag except krbpasswordexpiration, whose flag was
// not independently verified against a live server — see
// ipa_common.go's own doc comment on the flag-mapping methodology;
// this port maps it to `--krbpasswordexpiration` on the strength of
// FreeIPA's CLI-flags-mirror-API-params convention verified elsewhere,
// documented here as the one attribute in this module not individually
// confirmed); mail, telephonenumber, sshpubkey (CLI `--ipasshpubkey`),
// userauthtype (CLI `--ipauserauthtype`) — all lists, each fully
// replacing the attribute's current value set when given (an empty
// list clears it, matching real ipa_user's own documented "empty list
// deletes all values" contract for each of these); password (CLI
// `--userpassword`) and update_password (always|on_create, default
// "always") — password is sent on create unconditionally when given;
// on an already-existing user it is sent only when update_password is
// "always" (the default) — and, since this port cannot read back a
// stored password hash to compare, EVERY run with update_password=
// always and password set sends `--userpassword` again and is reported
// Changed, exactly matching real ipa_user's own equivalent inherent
// limitation (it cannot verify a password is already correct either,
// for the same reason: FreeIPA never returns password hashes).
//
// Idempotency for every other scalar/list attribute is a real
// query-then-diff: `ipa user-show <uid> --all --raw` is parsed and
// compared field-by-field (ipaScalarDiff/ipaListDiff) before building
// one combined `user-mod` call — a no-op run (nothing given differs)
// makes no `user-mod` call at all and reports unchanged.
//
// state=enabled/disabled is checked against the user's own raw
// `nsaccountlock` attribute (`TRUE` means disabled) and applied via
// `user-enable`/`user-disable` — separately from, and in addition to,
// any attribute changes above.
func moduleIpaUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	uid := argString(args, "uid", argString(args, "name", ""))
	if uid == "" {
		return Result{}, errArg("ipa_user: uid (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "enabled" && state != "disabled" {
		return Result{}, errArg("ipa_user: state must be present, absent, enabled, or disabled, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	updatePassword := argString(args, "update_password", "always")
	if updatePassword != "always" && updatePassword != "on_create" {
		return Result{}, errArg("ipa_user: update_password must be always or on_create, got %q", updatePassword)
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_user"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "user", uid)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(uid + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "user-del", uid)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_user", "user-del", res), nil
		}
		return Changed(uid + " removed"), nil
	}

	changed := false

	if !present {
		givenname := argString(args, "givenname", "")
		sn := argString(args, "sn", "")
		if givenname == "" || sn == "" {
			return Result{}, errArg("ipa_user: givenname and sn are required to create a new user %q", uid)
		}
		flags := []string{"user-add", uid, "--givenname=" + givenname, "--sn=" + sn}
		flags = append(flags, ipaUserScalarCreateFlags(args)...)
		flags = append(flags, ipaUserListCreateFlags(args)...)
		if pw := argString(args, "password", ""); pw != "" {
			flags = append(flags, "--userpassword="+pw)
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_user", "user-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "user", uid)
		if err != nil {
			return Result{}, err
		}
	} else {
		var modFlags []string
		for _, spec := range ipaUserScalarSpecs {
			if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flag)
			}
		}
		for _, spec := range ipaUserListSpecs {
			if flags, has := ipaListDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flags...)
			}
		}
		if pw := argString(args, "password", ""); pw != "" && updatePassword == "always" {
			modFlags = append(modFlags, "--userpassword="+pw)
		}
		if len(modFlags) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"user-mod", uid}, modFlags...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_user", "user-mod", res), nil
			}
			changed = true
			current, _, err = ipaShow(ctx, conn, "user", uid)
			if err != nil {
				return Result{}, err
			}
		}
	}

	locked := len(current["nsaccountlock"]) > 0 && strings.EqualFold(current["nsaccountlock"][0], "TRUE")
	if state == "disabled" && !locked {
		res, err := ipaRun(ctx, conn, "user-disable", uid)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_user", "user-disable", res), nil
		}
		changed = true
	} else if state == "enabled" && locked {
		res, err := ipaRun(ctx, conn, "user-enable", uid)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_user", "user-enable", res), nil
		}
		changed = true
	}

	if !changed {
		return Ok(uid + " already up to date"), nil
	}
	return Changed(uid + " updated"), nil
}

type ipaAttrSpec struct {
	arg, flag, raw string
}

// ipaUserScalarSpecs: ansible arg -> ipa CLI flag -> raw attribute
// name, for ipa_user's scalar attributes (see moduleIpaUser's own doc
// comment for which of these were individually verified).
var ipaUserScalarSpecs = []ipaAttrSpec{
	{"displayname", "displayname", "displayname"},
	{"gidnumber", "gidnumber", "gidnumber"},
	{"uidnumber", "uidnumber", "uidnumber"},
	{"homedirectory", "homedirectory", "homedirectory"},
	{"loginshell", "loginshell", "loginshell"},
	{"title", "title", "title"},
	{"krbpasswordexpiration", "krbpasswordexpiration", "krbpasswordexpiration"},
}

var ipaUserListSpecs = []ipaAttrSpec{
	{"mail", "mail", "mail"},
	{"telephonenumber", "telephonenumber", "telephonenumber"},
	{"sshpubkey", "ipasshpubkey", "ipasshpubkey"},
	{"userauthtype", "ipauserauthtype", "ipauserauthtype"},
}

func ipaUserScalarCreateFlags(args map[string]any) []string {
	var out []string
	for _, spec := range ipaUserScalarSpecs {
		if v := argString(args, spec.arg, ""); v != "" {
			out = append(out, "--"+spec.flag+"="+v)
		}
	}
	return out
}

func ipaUserListCreateFlags(args map[string]any) []string {
	var out []string
	for _, spec := range ipaUserListSpecs {
		if _, ok := args[spec.arg]; ok {
			if list := argStringList(args, spec.arg); len(list) > 0 {
				out = append(out, ipaFlagRepeat(spec.flag, list)...)
			}
		}
	}
	return out
}
