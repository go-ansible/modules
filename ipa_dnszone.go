package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaDnszone implements (a subset of) Ansible's `ipa_dnszone`
// module: manages a FreeIPA-managed DNS zone (an SOA record) via the
// real `ipa` CLI's own `dnszone-add`/`dnszone-mod`/`dnszone-del`/
// `dnszone-show` subcommands. See ipa_common.go's own doc comment for
// this port's shared architecture and the connection-argument gap.
//
// Args: zone_name (string, required); allowsyncptr (bool, default
// false) -> `--idnsallowsyncptr` (verified — NOT `--allowsyncptr`);
// dynamicupdate (bool, default false) -> `--idnsallowdynupdate`
// (verified — NOT `--dynamicupdate`); state (present|absent, default
// "present").
//
// Idempotency is a real query-then-diff via `dnszone-show --all --raw`
// (ipaScalarDiff, comparing against the boolean's own TRUE/FALSE raw
// string) before issuing one `dnszone-mod` call — a run that changes
// nothing makes no `dnszone-mod` call and reports unchanged.
//
// Simplifications vs real ipa_dnszone: none of the SOA fine-tuning
// options real dnszone-add itself exposes (--idnssoarefresh/-retry/
// -expire/-minimum/-mname/-rname/-serial, --skip-nameserver-check) are
// in real ipa_dnszone's own argument spec either, so there is nothing
// to carry through here — this module's scope matches the real one's
// exactly.
func moduleIpaDnszone(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	zoneName, err := requireString(args, "zone_name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_dnszone: state must be present or absent, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_dnszone"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "dnszone", zoneName)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(zoneName + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "dnszone-del", zoneName)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_dnszone", "dnszone-del", res), nil
		}
		return Changed(zoneName + " removed"), nil
	}

	if !present {
		flags := []string{"dnszone-add", zoneName}
		if flag, ok := ipaBoolFlag(args, "allowsyncptr", "idnsallowsyncptr"); ok {
			flags = append(flags, flag)
		}
		if flag, ok := ipaBoolFlag(args, "dynamicupdate", "idnsallowdynupdate"); ok {
			flags = append(flags, flag)
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_dnszone", "dnszone-add", res), nil
		}
		return Changed(zoneName + " created"), nil
	}

	var modFlags []string
	if flag, has := ipaBoolDiff(args, "allowsyncptr", "idnsallowsyncptr", "idnsallowsyncptr", current); has {
		modFlags = append(modFlags, flag)
	}
	if flag, has := ipaBoolDiff(args, "dynamicupdate", "idnsallowdynupdate", "idnsallowdynupdate", current); has {
		modFlags = append(modFlags, flag)
	}
	if len(modFlags) == 0 {
		return Ok(zoneName + " already up to date"), nil
	}
	res, err := ipaRun(ctx, conn, append([]string{"dnszone-mod", zoneName}, modFlags...)...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return ipaFailedf("ipa_dnszone", "dnszone-mod", res), nil
	}
	return Changed(zoneName + " updated"), nil
}
