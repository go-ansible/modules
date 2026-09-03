package modules

import (
	"context"
	"path"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSystemdInfo implements Ansible's `systemd_info`
// (community.general) module: gathers read-only facts about systemd
// units (service/target/socket/mount/timer) into Extra["units"], a map
// keyed by unit name — read from real systemd_info.py's own source
// (not just its EXAMPLES/RETURN VALUES, per this batch's hard rule),
// since its exact property set and masked/not-found short-circuit are
// only visible in the implementation.
//
// Unlike systemd.go/systemd_service (state management), this module
// never changes anything and always reports unchanged — matching real
// systemd_info's own `module.exit_json(changed=False, ...)`.
//
// Args: unitname ([]string, default [] — every service/target/socket/
// mount/timer unit); each entry may be an exact unit name or a
// filepath.Match-style glob (real systemd_info uses Python's fnmatch,
// which is close enough to Go's path.Match for the '*'/'?'/'[...]'
// patterns this is typically given — same caveat mount_facts.go's own
// matchesAny documents). If NONE of the given patterns match any known
// unit, this port fails cleanly, matching real systemd_info's own
// module.fail_json for that case; a MIX of matching and non-matching
// patterns silently processes only the matches, also matching real
// systemd_info (its own non-matching list is returned internally but
// never surfaced as a failure or warning unless every pattern missed).
// extra_properties ([]string, default []) — additional `systemctl show`
// property names (case-insensitive; always returned lower-cased,
// matching real systemd_info's own key lower-casing) appended to each
// unit's own per-category base set; if a unit is found but a requested
// extra property doesn't exist for it, this port fails cleanly (Result
// {Failed:true}), matching real systemd_info's own behavior exactly.
//
// Per-unit fields: name, loadstate, activestate, substate always;
// units whose loadstate is "not-found" or "masked" get ONLY those four
// fields (matching real systemd_info's own documented minimal-property
// short-circuit); every other unit additionally gets its category's
// base properties (service: fragmentpath, unitfilestate,
// unitfilepreset, mainpid, execmainpid; target/socket/timer:
// fragmentpath, unitfilestate, unitfilepreset; mount: where, what,
// options, type) plus any extra_properties, all queried via one
// `systemctl show -p <props> -- <unit>` per unit and parsed as
// lower-cased KEY=value lines (first occurrence of a repeated key
// wins, matching real systemd_info's own parse_show_output).
//
// Simplifications vs real systemd_info: this port skips the initial
// `systemctl --version` call real systemd_info's own get_version()
// issues — it establishes nothing this port's implementation depends
// on (real systemd_info doesn't gate on its output either, it's called
// and its result discarded), so reproducing it would only add a wasted
// round trip.
func moduleSystemdInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	extraProps := argStringList(args, "extra_properties")

	out, err := run(ctx, conn, "systemctl list-units --no-pager --type service,target,socket,mount,timer "+
		"--all --plain --no-legend")
	if err != nil {
		return Result{}, err
	}
	unitsInfo := parseSystemdListUnits(out)

	allNames := make([]string, 0, len(unitsInfo))
	for name := range unitsInfo {
		allNames = append(allNames, name)
	}
	sort.Strings(allNames)

	var toProcess []string
	if patterns := argStringList(args, "unitname"); len(patterns) > 0 {
		matched := map[string]bool{}
		var nonMatching []string
		for _, p := range patterns {
			matchedAny := false
			for _, name := range allNames {
				if ok, _ := path.Match(p, name); ok {
					matched[name] = true
					matchedAny = true
				}
			}
			if !matchedAny {
				nonMatching = append(nonMatching, p)
			}
		}
		if len(matched) == 0 {
			return Fail("systemd_info: no units match any of the provided patterns: " + strings.Join(nonMatching, ", ")), nil
		}
		for name := range matched {
			toProcess = append(toProcess, name)
		}
		sort.Strings(toProcess)
	} else {
		toProcess = allNames
	}

	units := map[string]any{}
	for _, name := range toProcess {
		fact, failMsg, err := systemdInfoUnit(ctx, conn, name, unitsInfo[name], extraProps)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		units[name] = fact
	}
	return Ok("").WithExtra("units", units), nil
}

// parseSystemdListUnits parses `systemctl list-units ... --plain
// --no-legend` output: whitespace-separated "unit loadstate activestate
// substate description...", one map per unit keyed by unit name.
func parseSystemdListUnits(out string) map[string]map[string]string {
	units := map[string]map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		units[fields[0]] = map[string]string{
			"name":        fields[0],
			"loadstate":   fields[1],
			"activestate": fields[2],
			"substate":    fields[3],
		}
	}
	return units
}

// systemdInfoCategoryProps returns the category-specific `systemctl
// show` property names (in their real, mixed-case CLI form) for a unit
// name based on its suffix, or nil if unrecognized.
func systemdInfoCategoryProps(unit string) []string {
	switch {
	case strings.HasSuffix(unit, ".service"):
		return []string{"FragmentPath", "UnitFileState", "UnitFilePreset", "MainPID", "ExecMainPID"}
	case strings.HasSuffix(unit, ".mount"):
		return []string{"Where", "What", "Options", "Type"}
	case strings.HasSuffix(unit, ".target"), strings.HasSuffix(unit, ".socket"), strings.HasSuffix(unit, ".timer"):
		return []string{"FragmentPath", "UnitFileState", "UnitFilePreset"}
	default:
		return nil
	}
}

// systemdInfoUnit builds one unit's fact map: the minimal four fields
// for a not-found/masked unit (or one missing from unitsInfo entirely,
// which this port treats the same way — real systemd_info's own
// process_unit falls back to the units_info dict's own zero-value
// default in that case), or the full base+extra property set otherwise.
// A non-empty failMsg means real systemd_info would module.fail_json
// here (an unrecognized unit suffix, or a missing extra_properties
// entry).
func systemdInfoUnit(ctx context.Context, conn remoteexec.Connection, name string, base map[string]string,
	extraProps []string) (fact map[string]any, failMsg string, err error) {
	loadstate := strings.ToLower(base["loadstate"])
	if base == nil || loadstate == "not-found" || loadstate == "masked" {
		if base == nil {
			return map[string]any{"name": name, "loadstate": "not-found"}, "", nil
		}
		return map[string]any{
			"name":        name,
			"loadstate":   base["loadstate"],
			"activestate": base["activestate"],
			"substate":    base["substate"],
		}, "", nil
	}

	category := systemdInfoCategoryProps(name)
	if category == nil {
		return nil, "systemd_info: could not determine the category for unit '" + name + "'", nil
	}

	props := append([]string{"LoadState", "ActiveState", "SubState"}, category...)
	props = append(props, extraProps...)
	props = systemdInfoDedup(props)

	out, err := run(ctx, conn, "systemctl show -p "+shellQuote(strings.Join(props, ","))+" -- "+shellQuote(name))
	if err != nil {
		return nil, "", err
	}
	unitData := parseSystemdShow(out)

	for _, p := range extraProps {
		if _, ok := unitData[strings.ToLower(p)]; !ok {
			return nil, "systemd_info: the following properties do not exist for unit '" + name + "': " + p, nil
		}
	}

	fact = map[string]any{"name": name}
	for _, k := range []string{"loadstate", "activestate", "substate"} {
		if v, ok := unitData[k]; ok {
			fact[k] = v
		}
	}
	if strings.ToLower(unitData["loadstate"]) != "not-found" && strings.ToLower(unitData["loadstate"]) != "masked" {
		for _, p := range props {
			k := strings.ToLower(p)
			if v, ok := unitData[k]; ok {
				fact[k] = v
			}
		}
	}
	return fact, "", nil
}

// parseSystemdShow parses `systemctl show` output ("Key=value" lines,
// one per property) into a map keyed by lower-cased property name; the
// first occurrence of a repeated key wins, matching real
// systemd_info's own parse_show_output.
func parseSystemdShow(out string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(key)
		if _, exists := result[key]; !exists {
			result[key] = val
		}
	}
	return result
}

func systemdInfoDedup(props []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(props))
	for _, p := range props {
		lp := strings.ToLower(p)
		if seen[lp] {
			continue
		}
		seen[lp] = true
		out = append(out, p)
	}
	return out
}
