package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaService implements (a subset of) Ansible's `ipa_service`
// module: manages a FreeIPA Kerberos service principal via the real
// `ipa` CLI's own `service-add`/`service-del`/`service-add-host`/
// `service-remove-host`/`service-show` subcommands. See
// ipa_common.go's own doc comment for this port's shared architecture
// and the connection-argument gap.
//
// Args: krbcanonicalname (string, required, aliased from name — the
// full principal, e.g. "http/host01.example.com"); force (bool) ->
// `--force` on `service-add` (create-time only: "force principal name
// even if host is not in DNS", matching real ipa_service's own
// documented scope for this argument); skip_host_check (bool) ->
// `--skip-host-check`, also create-time only, matching real
// ipa_service's own documented "only used on creation, not for
// updating existing services"; hosts (list of string, the service's
// "ManagedBy" hosts) — added via `service-add-host`'s own `--host`
// flag (following the same verified singular-flag-name-per-object-type
// convention as every other member-list command in this port — see
// ipa_common.go's own doc comment); state (present|absent, default
// "present").
//
// hosts is handled ADD-ONLY, the same deliberate, documented choice
// ipa_role.go makes for its own privilege argument and for the same
// reason: reading a service's CURRENT ManagedBy hosts back would need
// this port to know the exact raw attribute name FreeIPA's `service-
// show --raw` reports them under, which it could not verify against a
// live server (real ipa_service's own module_utils code reads this via
// the JSON-RPC response's own structured "managedby_host" field, not a
// name this port could confirm maps identically onto the CLI's raw
// LDAP attribute output). Rather than guess and risk silently never
// detecting (and so never removing) a stale ManagedBy host — unlike
// real ipa_service, which DOES reconcile hosts to exactly the given
// list — this port only ever adds every host listed via
// `service-add-host`, is always reported Changed=true when the list is
// non-empty (idempotent-safe on the FreeIPA side, since re-adding an
// already-managing host is a harmless no-op there), and never removes
// one — a narrower but honest behavior, not a silently wrong one.
//
// Real ipa_service has no other mutable scalar attribute in its own
// argument spec (no description, no update_password-style field) —
// there is nothing else for this port to reconcile on an
// already-present service beyond its ManagedBy hosts.
func moduleIpaService(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "krbcanonicalname", argString(args, "name", ""))
	if name == "" {
		return Result{}, errArg("ipa_service: krbcanonicalname (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_service: state must be present or absent, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_service"); !ok {
		return res, nil
	}

	_, present, err := ipaShow(ctx, conn, "service", name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(name + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "service-del", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_service", "service-del", res), nil
		}
		return Changed(name + " removed"), nil
	}

	changed := false

	if !present {
		flags := []string{"service-add", name}
		if argBool(args, "force", false) {
			flags = append(flags, "--force")
		}
		if argBool(args, "skip_host_check", false) {
			flags = append(flags, "--skip-host-check")
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_service", "service-add", res), nil
		}
		changed = true
	}

	// hosts is add-only — see moduleIpaService's own doc comment.
	if desired := argStringList(args, "hosts"); len(desired) > 0 {
		res, err := ipaRun(ctx, conn, append([]string{"service-add-host", name}, ipaFlagRepeat("host", desired)...)...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_service", "service-add-host", res), nil
		}
		changed = true
	}

	if !changed {
		return Ok(name + " already up to date"), nil
	}
	return Changed(name + " updated"), nil
}
