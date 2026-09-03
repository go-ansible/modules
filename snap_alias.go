package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSnapAlias implements (a subset of) Ansible's `snap_alias`
// module: manages command aliases for an installed snap.
//
// Args: name (string, required) — the snap; alias (string or
// []string) — the alias(es) to create/remove, required for
// state=present, optional for state=absent (real snap_alias removes
// ALL of the snap's aliases when alias is omitted on state=absent —
// this port preserves that special case); state (present|absent,
// default "present").
//
// Idempotency is checked via `snap aliases`, grepping each "<name>
// <alias> -" / "<name> <alias> manual" line, matching this batch's
// house pattern of a plain-text substring check over parsing the
// tool's full columnar output.
func moduleSnapAlias(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	aliases := argStringList(args, "alias")
	if len(aliases) == 0 {
		aliases = argStringList(args, "aliases")
	}
	state := argString(args, "state", "present")

	switch state {
	case "present":
		if len(aliases) == 0 {
			return Result{}, errArg("snap_alias: alias is required when state is present")
		}
		var toAdd []string
		for _, a := range aliases {
			present, err := snapAliasPresent(ctx, conn, name, a)
			if err != nil {
				return Result{}, err
			}
			if !present {
				toAdd = append(toAdd, a)
			}
		}
		if len(toAdd) == 0 {
			return Ok("aliases already present"), nil
		}
		for _, a := range toAdd {
			if _, err := run(ctx, conn, "snap alias "+shellQuote(name)+" "+shellQuote(a)); err != nil {
				return Result{}, err
			}
		}
		return Changed("added aliases"), nil

	case "absent":
		if len(aliases) == 0 {
			// Real snap_alias removes every alias for the snap when
			// none are given.
			if _, err := run(ctx, conn, "snap unalias "+shellQuote(name)); err != nil {
				return Result{}, err
			}
			return Changed("removed all aliases"), nil
		}
		var toRemove []string
		for _, a := range aliases {
			present, err := snapAliasPresent(ctx, conn, name, a)
			if err != nil {
				return Result{}, err
			}
			if present {
				toRemove = append(toRemove, a)
			}
		}
		if len(toRemove) == 0 {
			return Ok("aliases already absent"), nil
		}
		for _, a := range toRemove {
			if _, err := run(ctx, conn, "snap unalias "+shellQuote(a)); err != nil {
				return Result{}, err
			}
		}
		return Changed("removed aliases"), nil

	default:
		return Result{}, errArg("snap_alias: state must be present or absent, got %q", state)
	}
}

func snapAliasPresent(ctx context.Context, conn remoteexec.Connection, name, alias string) (bool, error) {
	res, err := runStatus(ctx, conn, "snap aliases 2>/dev/null | grep -qE "+shellQuote("^"+name+" +"+alias+" "))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
