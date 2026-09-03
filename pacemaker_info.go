package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacemakerInfo implements Ansible's `pacemaker_info` module:
// gathers Pacemaker cluster/resource/constraint/property/STONITH facts
// via the `pcs` CLI's own `--output-format=json` support (matching real
// pacemaker_info's own module_utils, _pacemaker, which wraps `pcs` —
// never `crm`), read-only.
//
// No args.
//
// Extra fields (matching real pacemaker_info's own return values):
// version (string, `pcs --version`); cluster_info, constraint_info,
// property_info, resource_info, stonith_info (each a decoded JSON value
// from `pcs <action> config --output-format=json`, for actions
// cluster/constraint/property/resource/stonith respectively — queried
// in that same alphabetical order real pacemaker_info's own
// `sorted(self.info_vars.items())` uses, though this port's order has
// no observable effect since every query is independent).
//
// Never Changed — this module only ever reads.
//
// Matching real pacemaker_info's own do_raise: if ANY of the five `pcs
// ... config --output-format=json` calls exits non-zero, the WHOLE
// module fails (Result{Failed:true}) rather than returning partial
// facts — real pacemaker_info has no notion of "some sections
// available, others not" either.
func modulePacemakerInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	versionRes, err := runStatus(ctx, conn, "pcs --version")
	if err != nil {
		return Result{}, err
	}
	if versionRes.RC != 0 {
		return Fail(fmt.Sprintf("pacemaker_info: pcs --version failed (rc=%d): %s", versionRes.RC, strings.TrimSpace(versionRes.Stderr))), nil
	}
	version := strings.TrimSpace(versionRes.Stdout)

	res := Ok("").WithExtra("version", version)

	for _, section := range []struct{ key, action string }{
		{"cluster_info", "cluster"},
		{"constraint_info", "constraint"},
		{"property_info", "property"},
		{"resource_info", "resource"},
		{"stonith_info", "stonith"},
	} {
		out, err := runStatus(ctx, conn, "pcs "+section.action+" config --output-format=json")
		if err != nil {
			return Result{}, err
		}
		if out.RC != 0 {
			return Fail(fmt.Sprintf("pacemaker_info: pcs %s config failed with error (rc=%d): %s", section.action, out.RC, strings.TrimSpace(out.Stderr))), nil
		}
		trimmed := strings.TrimSpace(out.Stdout)
		if trimmed == "" || trimmed == `""` {
			res = res.WithExtra(section.key, nil)
			continue
		}
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
			return Fail(fmt.Sprintf("pacemaker_info: parsing pcs %s config JSON output: %v", section.action, err)), nil
		}
		res = res.WithExtra(section.key, decoded)
	}

	return res, nil
}
