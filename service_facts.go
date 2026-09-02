package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleServiceFacts implements (a subset of) Ansible's `service_facts`
// module: gathers services into Extra["services"], shaped like real
// ansible_facts.services — a map from unit name to a map with "name",
// "state" (started|stopped, derived from systemd's ACTIVE column), and
// "source" ("systemd").
//
// Args: none.
//
// Only systemd-managed hosts are supported (checked via `command -v
// systemctl`). Real ansible.builtin.service_facts additionally supports
// SysV, OpenRC, AIX SRC, and Solaris SMF backends via a chain of Python
// implementations tried in turn; this port implements only the systemd
// path — the modern majority case — and fails cleanly (rather than
// silently returning an empty list) when systemctl isn't found. This is
// a real simplification versus real Ansible's broader backend coverage,
// not a claim of equivalence.
func moduleServiceFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	res, err := runStatus(ctx, conn, "command -v systemctl >/dev/null 2>&1")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("service_facts: systemctl not found (only systemd-managed hosts are supported by this port)"), nil
	}

	out, err := run(ctx, conn, "systemctl list-units --type=service --all --no-legend --plain")
	if err != nil {
		return Result{}, err
	}

	services := map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "●" { // a leading "●" marks a failed unit, shifting the columns by one
			fields = fields[1:]
		}
		if len(fields) < 4 {
			continue
		}
		name := fields[0]
		active := fields[2]
		state := "stopped"
		if active == "active" {
			state = "started"
		}
		services[name] = map[string]any{
			"name":   name,
			"state":  state,
			"source": "systemd",
		}
	}
	return Ok("").WithExtra("services", services), nil
}
