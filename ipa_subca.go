package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaSubca implements Ansible's `ipa_subca` module: adds,
// modifies, disables, enables, and deletes FreeIPA Lightweight Sub
// Certificate Authorities via the real `ipa` CLI's own `ca-add`/
// `ca-mod`/`ca-del`/`ca-disable`/`ca-enable`/`ca-show` subcommands. See
// ipa_common.go's own doc comment for this port's shared architecture,
// including the connection-argument gap and the Kerberos-ticket
// precondition.
//
// Real ipa_subca's own JSON-RPC client (plugins/modules/ipa_subca.py's
// SubCAIPAClient) is named "subca_*" but every one of its methods
// actually calls the real API's `ca_add`/`ca_mod`/`ca_del`/`ca_find`/
// `ca_disable`/`ca_enable` — a sub-CA IS an `ipa ca-*` object, just one
// whose cn is not "ipa" (the root CA); this port shells out to the same
// `ca-*` subcommands the real module's own client methods ultimately
// call, verified directly from that source.
//
// Args, and their ansible-name -> raw-LDAP-attribute-name -> `ipa` CLI
// flag mapping (verified against FreeIPA's own published API command
// reference, freeipa.readthedocs.io/en/latest/api/ca_add.html):
//   - subca_name (required, aliased name) — the sub-CA's pkey (cn).
//   - subca_subject (required at create time; matching real
//     ipa_subca's own required=True) -> ipacasubjectdn ->
//     `--ipacasubjectdn`. Real ipa_subca does NOT allow modifying an
//     existing sub-CA's subject DN (verified from source: "IPA does not
//     allow to modify Sub CA's subject DN So skip it for now" — the
//     diff is computed but ipacasubjectdn is explicitly dropped from it
//     before ca-mod is ever built); this port does the same — a
//     subca_subject that differs from what's already set is silently
//     ignored on an existing sub-CA, never sent to `ca-mod`, and never
//     causes a failure or a reported change by itself.
//   - subca_desc -> description -> `--description`.
//   - state (present|absent|enabled|disabled, default present) — real
//     ipa_subca's own argument choices are literally "present"/
//     "absent"/"enabled"/"disabled" (its EXAMPLES block uses the
//     singular "enable"/"disable", which do not match its own
//     `choices=["present", "absent", "enabled", "disabled"]` and would
//     be rejected by AnsibleModule's own choice validation — a
//     real-module documentation bug this port does not replicate, since
//     replicating a doc/example mismatch would just mean silently
//     rejecting the plural spelling too; this port accepts exactly the
//     four real argspec choices).
//
// Idempotency: `ca-show <subca_name> --all --raw` is parsed; on create,
// subca_subject/subca_desc are sent; on an existing sub-CA, only
// subca_desc is diffed and (if changed) sent via `ca-mod` (subca_subject
// is never diffed or sent, per the doc comment above). state=enabled/
// disabled is checked against the sub-CA's own raw `ipacaenabled`
// attribute and applied via `ca-enable`/`ca-disable`, matching real
// ipa_subca's own `subca_disable`/`subca_enable` calls — real ipa_subca
// runs these UNCONDITIONALLY whenever the sub-CA already exists and
// state is "disable"/"enable" (it does not check the current enabled
// state first, since `ca_disable`/`ca_enable` are themselves idempotent
// server-side); this port does the same rather than pre-checking
// `ipacaenabled`, since this port could not independently verify that
// raw attribute name against FreeIPA's own API reference (ca_show.html's
// own attribute list was not confirmed for this batch) — sending
// ca-disable/ca-enable unconditionally is the SAFE direction here (an
// already-disabled/-enabled sub-CA re-disabled/re-enabled is a
// server-side no-op, not a wrong state change), so state=enabled/
// disabled is ALWAYS reported Changed when the sub-CA exists, matching
// real ipa_subca's own unconditional-call behavior exactly.
//
// Deviation vs real ipa_subca: real ipa_subca returns the full
// post-change sub-CA dict as its own `record` return value (note: not
// `subca`, despite the module's RETURN VALUES documentation header
// saying `subca` — verified from source: `module.exit_json(changed=changed,
// record=record)`, a real doc/code mismatch in ipa_subca itself); this
// port does not surface that dict in Result.Extra at all — only
// changed/failed/msg, matching every other ipa_* module already shipped
// in this port.
func moduleIpaSubca(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "subca_name", argString(args, "name", ""))
	if name == "" {
		return Result{}, errArg("ipa_subca: subca_name (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "enabled" && state != "disabled" {
		return Result{}, errArg("ipa_subca: state must be present, absent, enabled, or disabled, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_subca"); !ok {
		return res, nil
	}

	_, present, err := ipaShow(ctx, conn, "ca", name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(name + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "ca-del", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_subca", "ca-del", res), nil
		}
		return Changed(name + " removed"), nil
	}

	changed := false

	if !present {
		subject := argString(args, "subca_subject", "")
		if subject == "" {
			return Result{}, errArg("ipa_subca: subca_subject is required to create a new sub-CA %q", name)
		}
		flags := []string{"ca-add", name, "--ipacasubjectdn=" + subject}
		if d := argString(args, "subca_desc", ""); d != "" {
			flags = append(flags, "--description="+d)
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_subca", "ca-add", res), nil
		}
		changed = true
	} else {
		current, _, err := ipaShow(ctx, conn, "ca", name)
		if err != nil {
			return Result{}, err
		}
		if flag, has := ipaScalarDiff(args, "subca_desc", "description", "description", current); has {
			res, err := ipaRun(ctx, conn, "ca-mod", name, flag)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return ipaFailedf("ipa_subca", "ca-mod", res), nil
			}
			changed = true
		}
	}

	if state == "disabled" {
		res, err := ipaRun(ctx, conn, "ca-disable", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_subca", "ca-disable", res), nil
		}
		changed = true
	} else if state == "enabled" {
		res, err := ipaRun(ctx, conn, "ca-enable", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_subca", "ca-enable", res), nil
		}
		changed = true
	}

	if !changed {
		return Ok(name + " already up to date"), nil
	}
	return Changed(name + " updated"), nil
}
