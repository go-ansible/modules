package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaOtptoken implements Ansible's `ipa_otptoken` module: adds,
// modifies, and deletes FreeIPA One Time Password tokens via the real
// `ipa` CLI's own `otptoken-add`/`otptoken-mod`/`otptoken-del`/
// `otptoken-show` subcommands. See ipa_common.go's own doc comment for
// this port's shared architecture, including the connection-argument
// gap and the Kerberos-ticket precondition.
//
// Args, and their ansible-name -> raw-LDAP-attribute-name -> `ipa` CLI
// flag mapping (all read directly from real ipa_otptoken's own source,
// plugins/modules/ipa_otptoken.py's `ansible_to_ipa` dict, then each
// flag individually confirmed against FreeIPA's own published API
// command reference, freeipa.readthedocs.io/en/latest/api/otptoken_add.html
// and .../otptoken_mod.html — the strongest evidence available for this
// batch, since ansible_to_ipa IS the real module's own ground truth for
// the raw attribute name, and every one of those raw names was
// independently confirmed to map to `--<raw-name>` on the real `ipa`
// CLI with no exception, except `rename` below):
//   - uniqueid (required, aliased name) -> ipatokenuniqueid (pkey; the
//     positional argument to `otptoken-show`/`-add`/`-del`, matching
//     ipaShow's own resource/pkey convention).
//   - newuniqueid -> rename -> `--rename` on `otptoken-mod` ONLY
//     (otptoken_mod.html confirms a `rename` option; otptoken_add.html
//     has none, matching real ipa_otptoken's own logic, which only ever
//     sends "rename" through otptoken_mod, never otptoken_add — see the
//     create-time quirk documented below).
//   - otptype (choices totp|hotp; CANNOT be modified after creation,
//     matching real ipa_otptoken's own `unmodifiable_after_creation`
//     list) -> type (uppercased — real ipa_otptoken does
//     `otptype.upper()` before sending; this port does the same)
//     -> `--type`.
//   - secretkey (CANNOT be modified after creation) -> ipatokenotpkey
//     -> `--ipatokenotpkey`. DEVIATION vs real ipa_otptoken: real
//     ipa_otptoken base64-decodes the given secretkey and re-encodes it
//     as base32 before sending, because FreeIPA's JSON-RPC transport
//     wraps Bytes-typed parameters (like ipatokenotpkey) in a
//     "__base64__"/"__base32__" envelope that has no equivalent on the
//     real `ipa` CLI's own plain-text argument parsing (a CLI Bytes
//     argument is a fundamentally different wire encoding than a
//     JSON-RPC one) — this port could not verify how (or whether) the
//     `ipa` CLI's own Bytes-argument handling maps back to that same
//     base32 encoding without a live server to test against, so rather
//     than guess at a conversion this port cannot confirm, secretkey is
//     passed through to `--ipatokenotpkey` VERBATIM, exactly as given.
//     A caller supplying the same base64 string real ipa_otptoken
//     expects will very likely get a different on-disk key than real
//     ipa_otptoken would have generated; there is no known workaround
//     within this port's architecture.
//   - description -> description -> `--description`.
//   - owner -> ipatokenowner -> `--ipatokenowner`.
//   - enabled (bool, default true — ALWAYS effectively given, matching
//     real ipa_otptoken's own argspec default, not just "when present")
//     -> ipatokendisabled = NOT enabled (INVERTED — verified from
//     source: `otptoken[...] = not enabled`) -> `--ipatokendisabled=
//     TRUE|FALSE`.
//   - notbefore, notafter (CANNOT be modified... actually these CAN be
//     modified per real ipa_otptoken, only otptype/secretkey/algorithm/
//     digits/offset/interval/counter cannot) -> ipatokennotbefore/
//     ipatokennotafter -> `--ipatokennotbefore`/`--ipatokennotafter`,
//     with a literal "Z" suffix appended (verified from source:
//     `f"{notbefore}Z"`) — this port appends it the same way.
//   - vendor, model, serial (informational only) -> ipatokenvendor/
//     ipatokenmodel/ipatokenserial -> `--ipatokenvendor`/
//     `--ipatokenmodel`/`--ipatokenserial`.
//   - algorithm (choices sha1|sha256|sha384|sha512; CANNOT be modified
//     after creation) -> ipatokenotpalgorithm -> `--ipatokenotpalgorithm`.
//   - digits (choices 6|8; CANNOT be modified after creation) ->
//     ipatokenotpdigits -> `--ipatokenotpdigits`.
//   - offset (CANNOT be modified after creation) ->
//     ipatokentotpclockoffset -> `--ipatokentotpclockoffset`.
//   - interval (CANNOT be modified after creation) ->
//     ipatokentotptimestep -> `--ipatokentotptimestep`.
//   - counter (CANNOT be modified after creation) -> ipatokenhotpcounter
//     -> `--ipatokenhotpcounter`.
//   - state (present|absent, default present).
//
// Create-time newuniqueid quirk (verified from real ipa_otptoken's own
// source and comment: "It would not make sense to have a rename after
// creation, so if the user specified a newuniqueid, just replace the
// uniqueid with the updated one before creation"): when the token named
// by uniqueid does not yet exist AND newuniqueid is given, real
// ipa_otptoken creates the token under newuniqueid's name instead of
// uniqueid's — this port replicates that exactly. Real ipa_otptoken
// also pre-checks (via a second otptoken_find) that newuniqueid is not
// already taken, hard-failing if it is; this port does the same via a
// second ipaShow.
//
// Unmodifiable-after-creation validation (otptype, secretkey, algorithm,
// digits, offset, interval, counter): matching real ipa_otptoken's own
// `validate_modifications`, when the token already exists and one of
// these is given with a value that DIFFERS from what `otptoken-show`
// currently reports, this port returns Result{Failed:true} (real
// ipa_otptoken calls module.fail_json for the same case — an expected,
// well-formed failure, not a Go error) — giving the same value that
// already matches is accepted silently (not resent), same as real
// ipa_otptoken's own diff-before-resend behavior.
//
// Deviation vs real ipa_otptoken: real ipa_otptoken returns the full
// post-change otptoken dict as its own `otptoken` return value; matching
// every other ipa_* module already shipped in this port, this port does
// not surface that dict in Result.Extra — only changed/failed/msg.
func moduleIpaOtptoken(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	uniqueid := argString(args, "uniqueid", argString(args, "name", ""))
	if uniqueid == "" {
		return Result{}, errArg("ipa_otptoken: uniqueid (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_otptoken: state must be present or absent, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	newuniqueid := argString(args, "newuniqueid", "")

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_otptoken"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "otptoken", uniqueid)
	if err != nil {
		return Result{}, err
	}

	if newuniqueid != "" {
		_, newPresent, err := ipaShow(ctx, conn, "otptoken", newuniqueid)
		if err != nil {
			return Result{}, err
		}
		if newPresent {
			return Fail("ipa_otptoken: requested rename through newuniqueid to " + newuniqueid +
				" failed because the new unique id is already in use"), nil
		}
	}

	if state == "absent" {
		if !present {
			return Ok(uniqueid + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "otptoken-del", uniqueid)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_otptoken", "otptoken-del", res), nil
		}
		return Changed(uniqueid + " removed"), nil
	}

	changed := false

	if !present {
		createName := uniqueid
		if newuniqueid != "" {
			createName = newuniqueid
		}
		flags := []string{"otptoken-add", createName}
		if otptype := argString(args, "otptype", ""); otptype != "" {
			flags = append(flags, "--type="+strings.ToUpper(otptype))
		}
		if sk := argString(args, "secretkey", ""); sk != "" {
			flags = append(flags, "--ipatokenotpkey="+sk)
		}
		flags = append(flags, ipaOtptokenScalarCreateFlags(args)...)
		enabled := argBool(args, "enabled", true)
		flags = append(flags, "--ipatokendisabled="+ipaOtptokenDisabledValue(enabled))
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_otptoken", "otptoken-add", res), nil
		}
		changed = true
	} else {
		if v := argString(args, "otptype", ""); v != "" {
			want := strings.ToUpper(v)
			have := ""
			if vals := current["type"]; len(vals) > 0 {
				have = vals[0]
			}
			if want != have {
				return Fail("ipa_otptoken: parameter 'otptype' cannot be changed once the OTP is created and " +
					"the requested value specified here (" + want + ") differs from what is set in the IPA " +
					"server (" + have + ")"), nil
			}
		}
		if conflict, want, have := ipaOtptokenUnmodifiableConflict(args, ipaAttrSpec{"secretkey", "ipatokenotpkey", "ipatokenotpkey"}, current); conflict {
			return Fail("ipa_otptoken: parameter 'secretkey' cannot be changed once the OTP is created and " +
				"the requested value specified here (" + want + ") differs from what is set in the IPA " +
				"server (" + have + ")"), nil
		}
		for _, spec := range ipaOtptokenUnmodifiableSpecs {
			if conflict, want, have := ipaOtptokenUnmodifiableConflict(args, spec, current); conflict {
				return Fail("ipa_otptoken: parameter '" + spec.arg + "' cannot be changed once the OTP is " +
					"created and the requested value specified here (" + want + ") differs from what is set " +
					"in the IPA server (" + have + ")"), nil
			}
		}

		var modFlags []string
		for _, spec := range ipaOtptokenModScalarSpecs {
			if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flag)
			}
		}
		enabled := argBool(args, "enabled", true)
		wantDisabled := ipaOtptokenDisabledValue(enabled)
		haveDisabled := ""
		if vals := current["ipatokendisabled"]; len(vals) > 0 {
			haveDisabled = strings.ToUpper(vals[0])
		}
		if wantDisabled != haveDisabled {
			modFlags = append(modFlags, "--ipatokendisabled="+wantDisabled)
		}
		if newuniqueid != "" {
			modFlags = append(modFlags, "--rename="+newuniqueid)
		}
		if len(modFlags) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"otptoken-mod", uniqueid}, modFlags...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_otptoken", "otptoken-mod", res), nil
			}
			changed = true
		}
	}

	if !changed {
		return Ok(uniqueid + " already up to date"), nil
	}
	return Changed(uniqueid + " updated"), nil
}

func ipaOtptokenDisabledValue(enabled bool) string {
	if enabled {
		return "FALSE"
	}
	return "TRUE"
}

// ipaOtptokenScalarCreateFlags builds the --flag=value tokens for
// otptoken-add's own scalar attrs shared with otptoken-mod (everything
// EXCEPT otptype/secretkey, which get special handling in
// moduleIpaOtptoken, and the "unmodifiable after creation" attrs, which
// also apply at create time via the same spec list used for diffing).
func ipaOtptokenScalarCreateFlags(args map[string]any) []string {
	var out []string
	for _, spec := range ipaOtptokenModScalarSpecs {
		if v := argString(args, spec.arg, ""); v != "" {
			out = append(out, "--"+spec.flag+"="+ipaOtptokenFormatValue(spec.arg, v))
		}
	}
	for _, spec := range ipaOtptokenUnmodifiableSpecs {
		if v := argString(args, spec.arg, ""); v != "" {
			out = append(out, "--"+spec.flag+"="+v)
		}
	}
	return out
}

// ipaOtptokenFormatValue appends real ipa_otptoken's own literal "Z"
// suffix for notbefore/notafter (see moduleIpaOtptoken's own doc
// comment) — every other scalar spec passes through unchanged.
func ipaOtptokenFormatValue(arg, v string) string {
	if arg == "notbefore" || arg == "notafter" {
		return v + "Z"
	}
	return v
}

var ipaOtptokenModScalarSpecs = []ipaAttrSpec{
	{"description", "description", "description"},
	{"owner", "ipatokenowner", "ipatokenowner"},
	{"notbefore", "ipatokennotbefore", "ipatokennotbefore"},
	{"notafter", "ipatokennotafter", "ipatokennotafter"},
	{"vendor", "ipatokenvendor", "ipatokenvendor"},
	{"model", "ipatokenmodel", "ipatokenmodel"},
	{"serial", "ipatokenserial", "ipatokenserial"},
}

// ipaOtptokenUnmodifiableSpecs: attrs real ipa_otptoken hard-refuses to
// change once the token exists (see moduleIpaOtptoken's own doc
// comment). algorithm/digits/offset/interval/counter values are sent
// verbatim (no notbefore/notafter-style suffix).
var ipaOtptokenUnmodifiableSpecs = []ipaAttrSpec{
	{"algorithm", "ipatokenotpalgorithm", "ipatokenotpalgorithm"},
	{"digits", "ipatokenotpdigits", "ipatokenotpdigits"},
	{"offset", "ipatokentotpclockoffset", "ipatokentotpclockoffset"},
	{"interval", "ipatokentotptimestep", "ipatokentotptimestep"},
	{"counter", "ipatokenhotpcounter", "ipatokenhotpcounter"},
}

// ipaOtptokenUnmodifiableConflict reports whether args[spec.arg] is
// given and differs from current[spec.raw]'s first value — used both
// for otptype/secretkey (checked separately, with otptype's own
// uppercasing) and for the algorithm/digits/offset/interval/counter
// specs above.
func ipaOtptokenUnmodifiableConflict(args map[string]any, spec ipaAttrSpec, current map[string][]string) (conflict bool, want, have string) {
	if _, ok := args[spec.arg]; !ok {
		return false, "", ""
	}
	want = argString(args, spec.arg, "")
	if vals := current[spec.raw]; len(vals) > 0 {
		have = vals[0]
	}
	if want == have {
		return false, want, have
	}
	return true, want, have
}
