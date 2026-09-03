package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSelinuxPermissive implements Ansible's `selinux_permissive`
// module: adds or removes a domain from SELinux's list of permissive
// domains.
//
// Args: domain (string, required; aliased from name); permissive (bool,
// required) — whether domain should (true) or should not (false) be
// permissive; no_reload (bool, default false) — real selinux_permissive
// disables reloading the SELinux policy after the change when true;
// store (string, default "") — the SELinux policy store to operate on.
//
// Real selinux_permissive is implemented entirely against the Python
// `seobject` binding (seobject.permissiveRecords), never shelling out
// to a CLI at all. This port has no such binding, only shell access, so
// it composes the `semanage permissive` command instead — the same
// underlying policycoreutils tool seobject itself wraps: `semanage
// permissive -l` to list the current permissive domains, `-a` to add,
// `-d` to delete, `-S store` to target a non-default store, and `-N` to
// suppress the post-commit policy reload when no_reload is true
// (semanage's own flag for exactly that; seobject's set_reload(False)
// achieves the same effect through the binding instead of a flag).
// Functionally equivalent, chosen for consistency with this port's
// other SELinux modules (selinux.go, seboolean.go), which take the same
// CLI-composition approach for the same reason.
//
// `semanage permissive -l` is parsed one domain per line (this port
// passes `-n` to suppress semanage's own heading line, so no header
// line needs to be recognized and skipped) — this exact plain listing
// shape is this port's own assumption about semanage's CLI output, not
// independently verified against a live SELinux system in this sandbox;
// a disclosed limitation shared with this batch's other `semanage`-based
// modules (sefcontext.go, selogin.go, seport.go).
func moduleSelinuxPermissive(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	domain := argString(args, "domain", argString(args, "name", ""))
	if domain == "" {
		return Result{}, errArg("selinux_permissive: domain (or its alias name) is required")
	}
	if _, ok := args["permissive"]; !ok {
		return Result{}, errArg("selinux_permissive: missing required argument: permissive")
	}
	permissive := argBool(args, "permissive", false)
	noReload := argBool(args, "no_reload", false)
	store := argString(args, "store", "")

	listCmd := "semanage permissive -n -l" + selinuxStoreFlag(store)
	out, err := run(ctx, conn, listCmd)
	if err != nil {
		return Result{}, err
	}
	present := false
	for _, l := range splitLines(out) {
		if strings.TrimSpace(l) == domain {
			present = true
			break
		}
	}

	changed := false
	if permissive && !present {
		cmd := "semanage permissive" + selinuxStoreFlag(store) + " -a " + shellQuote(domain) + selinuxNoReloadFlag(noReload)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	} else if !permissive && present {
		cmd := "semanage permissive" + selinuxStoreFlag(store) + " -d " + shellQuote(domain) + selinuxNoReloadFlag(noReload)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	}

	res := Result{Changed: changed}
	res = res.WithExtra("store", store).WithExtra("permissive", permissive).WithExtra("domain", domain)
	return res, nil
}

func selinuxStoreFlag(store string) string {
	if store == "" {
		return ""
	}
	return " -S " + shellQuote(store)
}

func selinuxNoReloadFlag(noReload bool) string {
	if noReload {
		return " -N"
	}
	return ""
}
