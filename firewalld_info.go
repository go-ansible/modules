package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFirewalldInfo implements (a subset of) Ansible's
// `firewalld_info` module: a gather-only counterpart to firewalld.go
// that reports firewalld's configuration without changing anything.
//
// Args: active_zones (bool, default false) — when true, report only
// the currently active zones (from `firewall-cmd --get-active-zones`);
// zones ([]string, optional) — when given (and active_zones is false),
// report only these zones, moving any that don't actually exist into
// Extra["undefined_zones"] instead of Extra["collected_zones"]; with
// neither given, every zone from `firewall-cmd --get-zones` is
// reported.
//
// Per-zone info is parsed from `firewall-cmd --zone=<z> --list-all`'s
// plain-text output (a fixed set of "key: value" lines) rather than
// firewalld's D-Bus/Python API (which real firewalld_info uses
// directly) — this port has no such binding, only shell access, so it
// parses the CLI's human-readable summary instead. This is a narrower
// parse than real firewalld_info's structured API access: forward and
// masquerade are read as booleans ("yes"/"no"), interfaces/sources/
// services/ports/protocols/icmp_blocks as whitespace-split lists, and
// forward_ports/rich_rules are returned as raw (unparsed) strings
// rather than real firewalld_info's own structured sub-fields for each
// — documented here rather than silently flattened without comment.
func moduleFirewalldInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	activeZones := argBool(args, "active_zones", false)
	requestedZones := argStringList(args, "zones")

	defaultZone, err := run(ctx, conn, "firewall-cmd --get-default-zone")
	if err != nil {
		return Result{}, err
	}
	version, err := run(ctx, conn, "firewall-cmd --version")
	if err != nil {
		return Result{}, err
	}

	allZones, err := run(ctx, conn, "firewall-cmd --get-zones")
	if err != nil {
		return Result{}, err
	}
	allZoneSet := map[string]bool{}
	for _, z := range strings.Fields(allZones) {
		allZoneSet[z] = true
	}

	var collected, undefined []string
	switch {
	case activeZones:
		out, err := run(ctx, conn, "firewall-cmd --get-active-zones")
		if err != nil {
			return Result{}, err
		}
		collected = parseActiveZoneNames(out)
	case len(requestedZones) > 0:
		for _, z := range requestedZones {
			if allZoneSet[z] {
				collected = append(collected, z)
			} else {
				undefined = append(undefined, z)
			}
		}
	default:
		collected = strings.Fields(allZones)
	}

	zoneInfo := map[string]any{}
	for _, z := range collected {
		out, err := run(ctx, conn, "firewall-cmd --zone="+shellQuote(z)+" --list-all")
		if err != nil {
			return Result{}, err
		}
		zoneInfo[z] = parseFirewalldZone(out)
	}

	info := map[string]any{
		"default_zone": defaultZone,
		"version":      version,
		"zones":        zoneInfo,
	}
	res := Ok("").
		WithExtra("firewalld_info", info).
		WithExtra("active_zones", activeZones).
		WithExtra("collected_zones", collected).
		WithExtra("undefined_zones", undefined)
	return res, nil
}

// parseActiveZoneNames parses `firewall-cmd --get-active-zones`'s
// output: a zone name on its own line, followed by indented
// "interfaces:"/"sources:" lines that this port ignores (mount_facts.go
// takes the same "the common fields are what's implemented" approach).
func parseActiveZoneNames(out string) []string {
	var zones []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		zones = append(zones, strings.TrimSpace(line))
	}
	return zones
}

// parseFirewalldZone parses `firewall-cmd --zone=<z> --list-all`'s
// "key: value" lines (the first line, "<zone> (active)", is skipped).
func parseFirewalldZone(out string) map[string]any {
	z := map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "target":
			z["target"] = value
		case "icmp-block-inversion", "forward", "masquerade":
			z[strings.ReplaceAll(key, "-", "_")] = value == "yes"
		case "interfaces", "sources", "services", "ports", "protocols", "icmp-blocks":
			z[strings.ReplaceAll(key, "-", "_")] = strings.Fields(value)
		case "forward-ports", "source-ports", "rich rules":
			z[strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_")] = value
		}
	}
	return z
}
