package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaHost implements (a subset of) Ansible's `ipa_host` module:
// manages a FreeIPA host entry via the real `ipa` CLI's own
// `host-add`/`host-mod`/`host-del`/`host-disable`/`host-show`
// subcommands. See ipa_common.go's own doc comment for this port's
// shared architecture, the connection-argument gap, and the verified
// `ipa` CLI flag mapping — ip_address's `--ip-address` (hyphenated)
// and random_password's `--random` flag names were specifically
// verified there since they don't follow the arg-name-is-the-flag-name
// pattern most other options do.
//
// Args: fqdn (string, required, aliased from name); description,
// userclass, l (aliased locality), ns_host_location (aliased
// nshostlocation), ns_hardware_platform (aliased nshardwareplatform),
// ns_os_version (aliased nsosversion) — all scalar strings, mapped to
// the identically-named `ipa` CLI flag (nshostlocation etc — verified);
// mac_address (list, aliased macaddress), user_certificate (list,
// aliased usercertificate) — both fully replace the current value set
// when given, an empty list clearing it; force (bool) -> `--force`;
// force_creation (bool, default true) — when false, this port will NOT
// create a currently-absent host for state=enabled/disabled (only for
// state=present does absence always mean "create it"), matching real
// ipa_host's own documented "Create host if state=disabled or
// state=enabled but not present" semantics; ip_address (string) ->
// `--ip-address`; random_password (bool) -> `--random`, create-time
// only; update_dns (bool) — only meaningful with state=absent, ->
// `--updatedns` on `host-del`; state (present|absent|enabled|disabled,
// default "present").
//
// Idempotency for scalar/list attributes is a real query-then-diff via
// `host-show --all --raw` (ipaScalarDiff/ipaListDiff), combined into
// one `host-mod` call. Simplification vs real ipa_host: this port
// found no raw attribute on a host analogous to a user's own
// "nsaccountlock" that reliably indicates whether a host is currently
// disabled without a live server to check against, so state=disabled
// unconditionally calls `host-disable` every run — this is
// idempotency-safe in the ordinary sense (state ends up disabled
// either way) but a call against an ALREADY-disabled host may report
// Changed=true even when nothing actually changed, UNLESS `ipa`'s own
// output happens to mention "already" (checked as a best-effort
// signal, not a verified one). state=enabled is treated identically to
// state=present (ensure the host exists) — this port found no
// documented `host-enable` counterpart to re-activate a disabled
// host's keytab, matching how real ipa_host's own host life cycle
// works (re-enrollment, not a simple toggle).
func moduleIpaHost(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	fqdn := argString(args, "fqdn", argString(args, "name", ""))
	if fqdn == "" {
		return Result{}, errArg("ipa_host: fqdn (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "enabled" && state != "disabled" {
		return Result{}, errArg("ipa_host: state must be present, absent, enabled, or disabled, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	forceCreation := argBool(args, "force_creation", true)

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_host"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "host", fqdn)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(fqdn + " already absent"), nil
		}
		flags := []string{"host-del", fqdn}
		if argBool(args, "update_dns", false) {
			flags = append(flags, "--updatedns")
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_host", "host-del", res), nil
		}
		return Changed(fqdn + " removed"), nil
	}

	changed := false

	if !present {
		if state != "present" && !forceCreation {
			return Ok(fqdn + " absent and force_creation is false, skipping"), nil
		}
		flags := []string{"host-add", fqdn}
		flags = append(flags, ipaHostScalarCreateFlags(args)...)
		flags = append(flags, ipaHostListCreateFlags(args)...)
		if ip := argString(args, "ip_address", ""); ip != "" {
			flags = append(flags, "--ip-address="+ip)
		}
		if argBool(args, "force", false) {
			flags = append(flags, "--force")
		}
		if argBool(args, "random_password", false) {
			flags = append(flags, "--random")
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_host", "host-add", res), nil
		}
		changed = true
		current, _, err = ipaShow(ctx, conn, "host", fqdn)
		if err != nil {
			return Result{}, err
		}
	} else {
		var modFlags []string
		for _, spec := range ipaHostScalarSpecs {
			if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flag)
			}
		}
		for _, spec := range ipaHostListSpecs {
			if flags, has := ipaListDiff(args, spec.arg, spec.flag, spec.raw, current); has {
				modFlags = append(modFlags, flags...)
			}
		}
		if len(modFlags) > 0 {
			res, err := ipaRun(ctx, conn, append([]string{"host-mod", fqdn}, modFlags...)...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_host", "host-mod", res), nil
			}
			changed = true
			current, _, err = ipaShow(ctx, conn, "host", fqdn)
			if err != nil {
				return Result{}, err
			}
		}
	}

	if state == "disabled" {
		res, err := ipaRun(ctx, conn, "host-disable", fqdn)
		if err != nil {
			return Result{}, err
		}
		switch {
		case res.RC == 0:
			changed = true
		case strings.Contains(strings.ToLower(res.Stderr+res.Stdout), "already"):
			// best-effort: treat as already disabled, not a failure.
		default:
			return ipaFailedf("ipa_host", "host-disable", res), nil
		}
	}

	if !changed {
		return Ok(fqdn + " already up to date"), nil
	}
	return Changed(fqdn + " updated"), nil
}

var ipaHostScalarSpecs = []ipaAttrSpec{
	{"description", "description", "description"},
	{"userclass", "userclass", "userclass"},
	{"l", "l", "l"},
	{"ns_host_location", "nshostlocation", "nshostlocation"},
	{"ns_hardware_platform", "nshardwareplatform", "nshardwareplatform"},
	{"ns_os_version", "nsosversion", "nsosversion"},
}

var ipaHostListSpecs = []ipaAttrSpec{
	{"mac_address", "macaddress", "macaddress"},
	{"user_certificate", "usercertificate", "usercertificate"},
}

func ipaHostScalarCreateFlags(args map[string]any) []string {
	var out []string
	for _, spec := range ipaHostScalarSpecs {
		if v := argString(args, spec.arg, ""); v != "" {
			out = append(out, "--"+spec.flag+"="+v)
		}
	}
	return out
}

func ipaHostListCreateFlags(args map[string]any) []string {
	var out []string
	for _, spec := range ipaHostListSpecs {
		if _, ok := args[spec.arg]; ok {
			if list := argStringList(args, spec.arg); len(list) > 0 {
				out = append(out, ipaFlagRepeat(spec.flag, list)...)
			}
		}
	}
	return out
}
