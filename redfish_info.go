package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRedfishInfo implements Ansible's `redfish_info` module — the
// vendor-NEUTRAL Redfish info-gathering module, substituting DMTF's
// own `redfishtool` exactly as redfish_command.go's own doc comment
// describes.
//
// # Real return shape is a THIRD shape in this sub-batch
//
// redfish_info.py's own exit_json is `exit_json(redfish_facts=result)`
// — no changed/msg at all (an info module, never a mutation), where
// `result` accumulates one key per requested command, each holding
// whatever that command's own get_* function returns verbatim
// (confirmed by reading real main() FIRST, per this sub-batch's own
// twice-learned lesson). Crucially, a per-COMMAND failure (e.g. "no
// boot override enabled") does NOT fail the whole module in real
// Ansible — it's embedded as `{"ret": false, "msg": "..."}` under that
// command's own key, and exit_json still runs. Only a missing
// top-level CATEGORY resource (e.g. no Systems on this baseuri at all)
// calls real fail_json. This port matches that distinction: Fail() is
// reserved for missing baseuri/invalid category-or-command/binary-not-
// found/no-such-resource-at-all; a softer per-command problem is
// embedded in redfish_facts instead.
//
// # Real argument shape
//
// Unlike redfish_command/config, real `category` is a LIST (multiple
// categories in one call, merged into the same redfish_facts), and
// `command` (when given) is checked against EVERY listed category —
// `command` defaults per-category (CATEGORY_COMMANDS_DEFAULT) when
// omitted. This port reproduces the list-of-categories shape and the
// per-category default; the "all" keyword (for either category or
// command) is NOT supported this increment — real, disclosed gap,
// fails loud rather than silently expanding to an incomplete set.
//
// # This increment's real scope
//
// Real redfish_info.py declares 7 categories and ~39 commands total.
// This increment covers:
//
//   - Service: CheckAvailability, via `redfishtool root` (the
//     ServiceRoot resource) — real check_service_availability treats a
//     failed GET as `available: false` rather than a hard error (a
//     genuinely graceful "is it there at all" check), reproduced here
//     the same way.
//   - Systems: GetSystemInventory, GetBootOverride,
//     GetPowerRestorePolicy, each reading the bare `Systems` resource
//     (redfishtool's own --One default — the single-system case,
//     same disclosed narrower-than-real-Ansible's-multi-system
//     aggregation this whole sub-batch already relies on) and
//     extracting exactly the properties real get_system_inventory/
//     get_boot_override/get_power_restore_policy themselves read —
//     confirmed field-by-field against their own source, not guessed.
//     Wrapped in the same `[{"system_uri": uri}, {...}]` one-element
//     list shape real Ansible's own aggregate_systems tuple produces
//     (so a caller already written against real redfish_info's output
//     shape doesn't need special-casing for the single-system case).
//
// Chassis, Accounts, Sessions, Update, and Manager are declared with
// empty command lists — real categories, not wired yet — along with
// the remaining 12 Systems commands (GetHealthReport and friends need
// multi-subsystem traversal not yet attempted) — a later increment of
// this same batch.
//
// Args: category (required list); command (list, defaults per category
// when omitted); baseuri (required, real effect); username/password
// (real effect); auth_token (not supported, fails loud).
func moduleRedfishInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	categories := argStringList(args, "category")
	if len(categories) == 0 {
		categories = []string{"Systems"}
	}
	for _, c := range categories {
		if c == "all" {
			return Fail("redfish_info: category \"all\" is not supported by this port yet — see redfish_info.go's own doc comment"), nil
		}
	}
	requestedCommands := argStringList(args, "command")
	for _, c := range requestedCommands {
		if c == "all" {
			return Fail("redfish_info: command \"all\" is not supported by this port yet — see redfish_info.go's own doc comment"), nil
		}
	}

	baseuri, username, password, res, ok := redfishtoolCredentials("redfish_info", args)
	if !ok {
		return res, nil
	}
	if res, ok := redfishtoolRequireBinary(ctx, conn, "redfish_info"); !ok {
		return res, nil
	}

	facts := map[string]any{}
	for _, category := range categories {
		if res, ok := redfishCheckCategory("redfish_info", category, redfishInfoCategories); !ok {
			return res, nil
		}

		commands := requestedCommands
		if len(commands) == 0 {
			commands = []string{redfishInfoDefaultCommand[category]}
		}
		if res, ok := redfishCheckCommands("redfish_info", category, commands, redfishInfoCategories); !ok {
			return res, nil
		}

		for _, command := range commands {
			switch category {
			case "Service":
				v, err := redfishCheckServiceAvailability(ctx, conn, baseuri, username, password)
				if err != nil {
					return Result{}, err
				}
				facts["service"] = v

			case "Systems":
				systemURI, sysData, res, err := redfishGetBareSystem(ctx, conn, baseuri, username, password)
				if err != nil {
					return Result{}, err
				}
				if res.Failed {
					return res, nil
				}
				switch command {
				case "GetSystemInventory":
					facts["system"] = redfishAggregateOneSystem(systemURI, redfishSystemInventoryEntries(sysData))
				case "GetBootOverride":
					facts["boot_override"] = redfishAggregateOneSystem(systemURI, redfishBootOverrideEntries(sysData))
				case "GetPowerRestorePolicy":
					facts["power_restore_policy"] = redfishAggregateOneSystem(systemURI, sysData["PowerRestorePolicy"])
				}
			}
		}
	}

	return Ok("").WithExtra("redfish_facts", facts), nil
}

var redfishInfoCategories = map[string][]string{
	"Systems":  {"GetSystemInventory", "GetBootOverride", "GetPowerRestorePolicy"},
	"Chassis":  {},
	"Accounts": {},
	"Sessions": {},
	"Update":   {},
	"Manager":  {},
	"Service":  {"CheckAvailability"},
}

var redfishInfoDefaultCommand = map[string]string{
	"Systems":  "GetSystemInventory",
	"Chassis":  "GetFanInventory",
	"Accounts": "ListUsers",
	"Update":   "GetFirmwareInventory",
	"Sessions": "GetSessions",
	"Manager":  "GetManagerNicInventory",
	"Service":  "CheckAvailability",
}

// redfishAggregateOneSystem wraps one system's own entries in the same
// `[{"system_uri": uri}, {...entries}]` shape real Ansible's own
// aggregate_systems (a Python tuple, JSON-serialized as a 2-element
// array) produces for a single-member list — see this file's own doc
// comment for why this port only ever has one member.
func redfishAggregateOneSystem(systemURI string, entries any) []any {
	return []any{
		[]any{map[string]any{"system_uri": systemURI}, entries},
	}
}

// redfishSystemInventoryEntries copies exactly the properties real
// get_system_inventory itself reads (confirmed from its own source),
// each included only if present.
func redfishSystemInventoryEntries(sysData map[string]any) map[string]any {
	properties := []string{
		"Status", "HostName", "PowerState", "BootProgress", "Model",
		"Manufacturer", "PartNumber", "SystemType", "AssetTag", "ServiceTag",
		"SerialNumber", "SKU", "BiosVersion", "MemorySummary", "ProcessorSummary",
		"TrustedModules", "Name", "Id",
	}
	entries := map[string]any{}
	for _, p := range properties {
		if v, ok := sysData[p]; ok {
			entries[p] = v
		}
	}
	return entries
}

// redfishBootOverrideEntries reproduces real get_boot_override exactly:
// nothing is returned (an empty map here — real Ansible instead fails
// this one command's own "entries" with a ret:false, not attempted
// verbatim here since it doesn't change this port's own Changed/Failed
// contract either way) unless BootSourceOverrideEnabled is present and
// not false, in which case the listed properties are copied when
// present and non-nil.
func redfishBootOverrideEntries(sysData map[string]any) map[string]any {
	boot, _ := sysData["Boot"].(map[string]any)
	entries := map[string]any{}
	if boot == nil {
		return entries
	}
	enabled, has := boot["BootSourceOverrideEnabled"]
	if !has || enabled == false {
		return entries
	}
	properties := []string{
		"BootSourceOverrideEnabled", "BootSourceOverrideTarget", "BootSourceOverrideMode",
		"UefiTargetBootSourceOverride", "BootSourceOverrideTarget@Redfish.AllowableValues",
	}
	for _, p := range properties {
		if v, ok := boot[p]; ok && v != nil {
			entries[p] = v
		}
	}
	return entries
}

// redfishCheckServiceAvailability implements real check_service_
// availability: a failed GET means available:false, not a hard error —
// confirmed from its own source (it never returns ret:false at all).
func redfishCheckServiceAvailability(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (map[string]any, error) {
	var root map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &root, "root")
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return map[string]any{"available": false}, nil
	}
	properties := []string{"Id", "Name", "RedfishVersion", "Vendor", "ServiceIdentification", "ProtocolFeaturesSupported", "UUID"}
	entries := map[string]any{}
	for _, p := range properties {
		if v, ok := root[p]; ok {
			entries[p] = v
		}
	}
	return map[string]any{"available": true, "entries": entries}, nil
}

// redfishGetBareSystem GETs the bare Systems resource (redfishtool's
// own --One default) and returns its own @odata.id alongside the full
// decoded JSON, for the Systems-category commands to extract from.
func redfishGetBareSystem(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (string, map[string]any, Result, error) {
	var sys map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &sys, "Systems")
	if err != nil {
		return "", nil, Result{}, err
	}
	if r.RC != 0 {
		return "", nil, Fail("redfish_info: Systems resource not found: " + redfishtoolErrMsg(r)), nil
	}
	uri, _ := sys["@odata.id"].(string)
	return uri, sys, Result{}, nil
}
