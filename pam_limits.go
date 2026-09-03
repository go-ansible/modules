package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePamLimits implements (a subset of) Ansible's `pam_limits`
// (community.general) module: manages one entry in
// `/etc/security/limits.conf` (or another file given via `dest`),
// identified by the (domain, limit_type, limit_item) triple — unlike
// sysctl.go's single-field key, this module's "key" is three fields.
//
// Args: domain (string, required) — a username, "@groupname", wildcard,
// or UID/GID range; limit_type (string, required) — "hard", "soft", or
// "-"; limit_item (string, required) — one of the limits.conf item
// names (core, nofile, nproc, ...; this port does not validate against
// the closed choice list real pam_limits enforces — an unrecognized
// item is written through as-is rather than rejected); value (string,
// required) — "unlimited"/"infinity"/"-1" or a number; comment (string,
// default "") — appended to the line as "\t# <comment>"; dest (string,
// default "/etc/security/limits.conf"); backup (bool, default false);
// use_max, use_min (bool, default false each) — when the domain/type/
// item already has a value, keep the larger (use_max) or smaller
// (use_min) of the existing and requested value instead of
// unconditionally overwriting it ("unlimited"/"infinity"/"-1" always
// compare as larger than any finite number, matching their meaning as
// "no limit").
//
// Real pam_limits has no `state` argument at all — like debconf.go's
// module, there is no documented way to remove an entry, only to
// ensure one exists with a given (possibly use_max/use_min-adjusted)
// value; this port matches that (always upserts, never deletes).
//
// Simplification: the `comment` argument is treated as authoritative
// whenever this port rewrites a matched line — an omitted `comment`
// (its default "") clears any comment the existing line had, rather
// than preserving it. Real pam_limits' own comment-preservation
// behavior on partial re-runs was not independently verified against
// its source; this is a documented, predictable narrowing rather than a
// silent guess.
func modulePamLimits(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	domain, err := requireString(args, "domain")
	if err != nil {
		return Result{}, err
	}
	limitType, err := requireString(args, "limit_type")
	if err != nil {
		return Result{}, err
	}
	limitItem, err := requireString(args, "limit_item")
	if err != nil {
		return Result{}, err
	}
	value, err := requireString(args, "value")
	if err != nil {
		return Result{}, err
	}
	comment := argString(args, "comment", "")
	dest := argString(args, "dest", "/etc/security/limits.conf")
	backup := argBool(args, "backup", false)
	useMax := argBool(args, "use_max", false)
	useMin := argBool(args, "use_min", false)

	res, err := runStatus(ctx, conn, "cat "+shellQuote(dest)+" 2>/dev/null")
	if err != nil {
		return Result{}, err
	}
	var lines []string
	if res.RC == 0 {
		lines = splitLines(res.Stdout)
	}

	newLines, changed := pamLimitsApplyEntry(lines, domain, limitType, limitItem, value, comment, useMax, useMin)
	if !changed {
		return Ok(domain + " " + limitType + " " + limitItem + " unchanged"), nil
	}

	if backup {
		if _, err := run(ctx, conn, "cp "+shellQuote(dest)+" "+shellQuote(dest)+".$(date +%Y%m%d%H%M%S) 2>/dev/null"); err != nil {
			return Result{}, err
		}
	}
	content := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		content += "\n"
	}
	if _, err := conn.Exec(ctx, "cat > "+shellQuote(dest), strings.NewReader(content)); err != nil {
		return Result{}, err
	}
	return Changed(domain + " " + limitType + " " + limitItem), nil
}

// pamLimitsApplyEntry replaces (or appends) the limits.conf line for
// (domain, limitType, limitItem), applying use_max/use_min against an
// existing value if found.
func pamLimitsApplyEntry(lines []string, domain, limitType, limitItem, value, comment string, useMax, useMin bool) ([]string, bool) {
	out := make([]string, len(lines))
	copy(out, lines)

	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 4 || fields[0] != domain || fields[1] != limitType || fields[2] != limitItem {
			continue
		}
		existingValue := fields[3]
		finalValue := value
		switch {
		case useMax:
			if pamLimitsCompareValue(existingValue, value) >= 0 {
				finalValue = existingValue
			}
		case useMin:
			if pamLimitsCompareValue(existingValue, value) <= 0 {
				finalValue = existingValue
			}
		}
		newLine := domain + " " + limitType + " " + limitItem + " " + finalValue
		if comment != "" {
			newLine += "\t# " + comment
		}
		if out[i] == newLine {
			return out, false
		}
		out[i] = newLine
		return out, true
	}

	newLine := domain + " " + limitType + " " + limitItem + " " + value
	if comment != "" {
		newLine += "\t# " + comment
	}
	out = append(out, newLine)
	return out, true
}

// pamLimitsCompareValue compares two limits.conf values, treating
// "unlimited", "infinity", and "-1" as larger than any finite number
// (matching their meaning: no limit). Returns -1, 0, or 1 as a<b, a==b,
// a>b. A value that fails to parse as an integer and isn't one of the
// no-limit keywords is treated as equal to everything (a conservative
// "don't know" that never triggers a change on its own).
func pamLimitsCompareValue(a, b string) int {
	unlimited := func(s string) bool {
		switch s {
		case "unlimited", "infinity", "-1":
			return true
		}
		return false
	}
	aInf, bInf := unlimited(a), unlimited(b)
	switch {
	case aInf && bInf:
		return 0
	case aInf:
		return 1
	case bInf:
		return -1
	}
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	if aerr != nil || berr != nil {
		return 0
	}
	switch {
	case an < bn:
		return -1
	case an > bn:
		return 1
	default:
		return 0
	}
}
