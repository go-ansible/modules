package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePipPackageInfo implements Ansible's `pip_package_info` module:
// gathers installed package facts for one or more pip executables via
// `<client> list -l --format=json`, read-only — matching real
// pip_package_info's own PIP.list_installed(), which runs exactly that
// command (real pip_package_info goes through Ansible's own
// module_utils.facts.packages.CLIMgr wrapper, but that wrapper's
// list_installed() is itself just this same `pip list -l --format=json`
// call).
//
// Args: clients ([]string, default ["pip"]) — pip executable names or
// paths; a client whose basename does not start with "pip" is skipped,
// matching real pip_package_info's own validation.
//
// Never Changed — this module only ever reads.
//
// Deviations vs real pip_package_info: real pip_package_info probes
// `is_available()` (a PATH lookup) before running each client and
// reports a skipped/failed client through Ansible's own module.warn
// channel (folded into the task's warnings, not its result fields);
// this package's Result has no warnings channel, so this port instead
// records a skipped-by-name-validation client in
// Extra["skipped_clients"] and a client that failed to run (missing
// executable, non-zero exit, unparseable JSON) in Extra["errors"], so
// the information is not silently dropped. Fails
// (Result{Failed:true}) only when EVERY requested client failed —
// exactly matching real pip_package_info's own found==0 case.
func modulePipPackageInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	clients := argStringList(args, "clients")
	if len(clients) == 0 {
		clients = []string{"pip"}
	}

	packages := map[string]any{}
	var skipped []string
	var errs []string

	for _, client := range clients {
		base := client
		if i := strings.LastIndexByte(client, '/'); i >= 0 {
			base = client[i+1:]
		}
		if !strings.HasPrefix(base, "pip") {
			skipped = append(skipped, client)
			continue
		}
		res, err := runStatus(ctx, conn, shellQuote(client)+" list -l --format=json")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			msg := strings.TrimSpace(res.Stderr)
			if msg == "" {
				msg = strings.TrimSpace(res.Stdout)
			}
			errs = append(errs, fmt.Sprintf("%s: rc=%d: %s", client, res.RC, msg))
			continue
		}
		var raw []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(res.Stdout), &raw); err != nil {
			errs = append(errs, fmt.Sprintf("%s: parsing JSON output: %v", client, err))
			continue
		}
		perClient := map[string]any{}
		for _, p := range raw {
			perClient[p.Name] = []any{map[string]any{"name": p.Name, "source": client, "version": p.Version}}
		}
		packages[client] = perClient
	}

	if len(packages) == 0 {
		return Fail(fmt.Sprintf("unable to use any of the supplied pip clients: %v", clients)), nil
	}

	res := Ok("").WithExtra("packages", packages)
	if len(skipped) > 0 {
		res = res.WithExtra("skipped_clients", skipped)
	}
	if len(errs) > 0 {
		res = res.WithExtra("errors", errs)
	}
	return res, nil
}
