package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpaVault implements Ansible's `ipa_vault` module: adds,
// modifies, and deletes FreeIPA vaults (KRA-backed secret storage) via
// the real `ipa` CLI's own `vault-add`/`vault-mod`/`vault-del`/
// `vault-show` subcommands. See ipa_common.go's own doc comment for
// this port's shared architecture, including the connection-argument
// gap and the Kerberos-ticket precondition.
//
// Args, and their ansible-name -> raw-LDAP-attribute-name -> `ipa` CLI
// flag mapping (verified against FreeIPA's own published API command
// reference, freeipa.readthedocs.io/en/latest/api/vault_add_internal.html):
//   - cn (required, aliased name) — the vault's pkey; cannot be changed
//     (matching real ipa_vault's own documentation: "Can not be changed
//     as it is the unique identifier").
//   - description -> description -> `--description`.
//   - ipavaulttype (aliased vault_type; choices asymmetric|standard|
//     symmetric, default "symmetric") -> ipavaulttype -> `--ipavaulttype`.
//   - ipavaultpublickey (aliased vault_public_key) -> ipavaultpublickey
//     -> `--ipavaultpublickey`.
//   - ipavaultsalt (aliased vault_salt) -> ipavaultsalt -> `--ipavaultsalt`.
//   - service (mutually exclusive with username) -> service -> `--service`.
//   - replace (bool, default false) — see the "no diffing without
//     replace=true" behavior below.
//   - state (present|absent, default present).
//
// ⚠ username/user is accepted (mutually exclusive with service, matching
// real ipa_vault's own argument spec) but has NO EFFECT WHATSOEVER — a
// real, verified quirk of real ipa_vault, not a bug in this port. Real
// ipa_vault's own `ensure()` function (plugins/modules/ipa_vault.py)
// reads it into a local variable and then never uses it again, with the
// module's own author leaving the line commented out and a TODO:
// `# user = module.params["username"]  TODO is this really not needed?`
// — `get_vault_dict` (which builds what actually gets sent to
// `vault_add_internal`/`vault_mod_internal`) never receives it, and
// `vault_find` only ever filters by cn. This port replicates that
// exactly: username/user is accepted for argument-shape compatibility
// (and the same mutual-exclusion check is enforced) but is never
// rendered into any `vault-add`/`vault-mod` flag, even though the real
// `ipa` CLI DOES have a working `--username` flag for this (verified
// against the same API reference above) — this port does not "fix" the
// gap and use it, since that would silently diverge from real
// ipa_vault's own observable behavior against a live server, which is
// what this port is a functional-parity port OF.
//
// ⚠ Without replace=true, an EXISTING vault is left completely
// unchanged no matter what description/ipavaulttype/ipavaultsalt/
// ipavaultpublickey/service values are given — real ipa_vault's own
// `ensure()` only computes a diff and calls `vault_mod_internal` inside
// an `if replace:` block; when replace is false (its own default), an
// existing vault's own diff is never even computed. This port replicates
// this exactly: on an existing vault, replace=false (the default) always
// reports unchanged and never calls `vault-mod`, regardless of what
// other args are given.
//
// Idempotency: `vault-show <cn> --all --raw` determines existence (a
// vault is either present or not; there is no further probe needed
// since replace=false never diffs and replace=true always sends
// whatever differs). On create, every given scalar arg above is sent;
// on an existing vault with replace=true, description/ipavaulttype/
// ipavaultsalt/ipavaultpublickey/service are diffed (ipaScalarDiff) and
// only sent via `vault-mod` when they differ — a no-op diff makes no
// `vault-mod` call at all and reports unchanged.
//
// Deviation vs real ipa_vault: real ipa_vault returns the full
// post-change vault dict as its own `vault` return value; matching
// every other ipa_* module already shipped in this port, this port does
// not surface that dict in Result.Extra — only changed/failed/msg.
func moduleIpaVault(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "cn", argString(args, "name", ""))
	if name == "" {
		return Result{}, errArg("ipa_vault: cn (or name) is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipa_vault: state must be present or absent, got %q", state)
	}
	if err := ipaProt(args); err != nil {
		return Result{}, err
	}
	_, hasUser := args["username"]
	if !hasUser {
		_, hasUser = args["user"]
	}
	if _, hasService := args["service"]; hasUser && hasService {
		return Result{}, errArg("ipa_vault: username (or user) and service are mutually exclusive")
	}
	replace := argBool(args, "replace", false)

	if res, ok := ipaRequireBinary(ctx, conn, "ipa_vault"); !ok {
		return res, nil
	}

	current, present, err := ipaShow(ctx, conn, "vault", name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(name + " already absent"), nil
		}
		res, err := ipaRun(ctx, conn, "vault-del", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_vault", "vault-del", res), nil
		}
		return Changed(name + " removed"), nil
	}

	if !present {
		flags := []string{"vault-add", name}
		for _, spec := range ipaVaultScalarSpecs {
			if v := argString(args, spec.arg, ""); v != "" {
				flags = append(flags, "--"+spec.flag+"="+v)
			}
		}
		res, err := ipaRun(ctx, conn, flags...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return ipaFailedf("ipa_vault", "vault-add", res), nil
		}
		return Changed(name + " created"), nil
	}

	if !replace {
		return Ok(name + " already exists"), nil
	}

	var modFlags []string
	for _, spec := range ipaVaultScalarSpecs {
		if flag, has := ipaScalarDiff(args, spec.arg, spec.flag, spec.raw, current); has {
			modFlags = append(modFlags, flag)
		}
	}
	if len(modFlags) == 0 {
		return Ok(name + " already up to date"), nil
	}
	res, err := ipaRun(ctx, conn, append([]string{"vault-mod", name}, modFlags...)...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return ipaFailedf("ipa_vault", "vault-mod", res), nil
	}
	return Changed(name + " updated"), nil
}

var ipaVaultScalarSpecs = []ipaAttrSpec{
	{"description", "description", "description"},
	{"ipavaulttype", "ipavaulttype", "ipavaulttype"},
	{"ipavaultsalt", "ipavaultsalt", "ipavaultsalt"},
	{"ipavaultpublickey", "ipavaultpublickey", "ipavaultpublickey"},
	{"service", "service", "service"},
}
