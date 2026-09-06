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
//   - Chassis: GetChassisInventory, GetFanInventory, GetChassisPower,
//     each reading the bare `Chassis` resource (same --One single-
//     chassis narrowing as Systems above) — GetFanInventory/
//     GetChassisPower each additionally discover and GET their own
//     Thermal/Power sub-resource, reproducing real get_fan_inventory/
//     get_chassis_power's exact soft-failure text ("No Fans present",
//     "Power information not found.") when the expected link or
//     property is missing, confirmed field-by-field from their own
//     source, not guessed.
//   - Accounts: ListUsers (list `AccountService Accounts`, GET each
//     member, filter empty account slots exactly as real list_users
//     does: UserName=="" and not Enabled) and GetAccountServiceConfig
//     (the entire raw AccountService resource verbatim, since real
//     get_accountservice_properties does nothing more than that GET).
//   - Sessions: GetSessions (list `SessionService Sessions`, GET each
//     member) — this category's only command, so a missing
//     SessionService/Sessions resource is this port's own category-
//     level hard fail rather than a soft per-command embed.
//
// Update and Manager are still declared with empty command lists — not
// wired yet — along with the remaining 12 Systems commands
// (GetHealthReport and friends need multi-subsystem traversal not yet
// attempted) and 5 more Chassis commands (GetChassisThermals,
// GetPsuInventory, GetHealthReport, and HPE-specific
// GetHPEThermalConfig/GetHPEFanPercentMin) — a later increment of this
// same batch.
//
// # A real bug this increment also fixed
//
// redfishGetBareSystem (and redfishResourceSubURI in redfish_config.go,
// and the ResetToDefaults discovery in redfish_command.go) called a
// bare `Systems`/`Managers` subcommand with no "-1" flag. Confirmed
// from SystemsMain/ManagersMain/ChassisMain's own source: a bare call
// with no operation argument AND no ID-selecting option defaults to
// redfishtool's own "collection" operation, not "get" — so those calls
// were decoding a `{Members:[...]}` collection into the single-
// resource shape their callers expect. Fixed by adding "-1" to all
// three call sites (and to this file's own new redfishGetBareChassis).
// AccountService/SessionService are unaffected: they're Redfish
// singletons (no Id-based collection), and their own Main functions
// default a bare call straight to "get" — confirmed separately.
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

			case "Chassis":
				_, chassisData, res, err := redfishGetBareChassis(ctx, conn, baseuri, username, password)
				if err != nil {
					return Result{}, err
				}
				if res.Failed {
					return res, nil
				}
				switch command {
				case "GetChassisInventory":
					facts["chassis"] = map[string]any{"ret": true, "entries": []any{redfishChassisInventoryEntry(chassisData)}}
				case "GetFanInventory":
					v, err := redfishGetFanInventory(ctx, conn, baseuri, username, password, chassisData)
					if err != nil {
						return Result{}, err
					}
					facts["fan"] = v
				case "GetChassisPower":
					v, err := redfishGetChassisPower(ctx, conn, baseuri, username, password, chassisData)
					if err != nil {
						return Result{}, err
					}
					facts["chassis_power"] = v
				}

			case "Accounts":
				accountService, res, err := redfishGetBareAccountService(ctx, conn, baseuri, username, password)
				if err != nil {
					return Result{}, err
				}
				if res.Failed {
					return res, nil
				}
				switch command {
				case "ListUsers":
					v, err := redfishListUsers(ctx, conn, baseuri, username, password)
					if err != nil {
						return Result{}, err
					}
					facts["user"] = v
				case "GetAccountServiceConfig":
					facts["accountservice_config"] = map[string]any{"ret": true, "entries": accountService}
				}

			case "Sessions":
				if command == "GetSessions" {
					v, res, err := redfishGetSessions(ctx, conn, baseuri, username, password)
					if err != nil {
						return Result{}, err
					}
					if res.Failed {
						return res, nil
					}
					facts["session"] = v
				}
			}
		}
	}

	return Ok("").WithExtra("redfish_facts", facts), nil
}

var redfishInfoCategories = map[string][]string{
	"Systems":  {"GetSystemInventory", "GetBootOverride", "GetPowerRestorePolicy"},
	"Chassis":  {"GetChassisInventory", "GetFanInventory", "GetChassisPower"},
	"Accounts": {"ListUsers", "GetAccountServiceConfig"},
	"Sessions": {"GetSessions"},
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
	// "-1" (--One) is required: a bare "Systems" call with no operation
	// argument and no ID-selecting option defaults to redfishtool's own
	// "collection" operation, not "get" — confirmed from SystemsMain's
	// own source. Without it this decodes a {Members:[...]} collection
	// into the single-system shape this function's callers expect.
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &sys, "-1", "Systems")
	if err != nil {
		return "", nil, Result{}, err
	}
	if r.RC != 0 {
		return "", nil, Fail("redfish_info: Systems resource not found: " + redfishtoolErrMsg(r)), nil
	}
	uri, _ := sys["@odata.id"].(string)
	return uri, sys, Result{}, nil
}

// redfishGetBareChassis GETs the bare Chassis resource (redfishtool's
// own --One default, "-1" required for the same reason documented on
// redfishGetBareSystem) and returns its own @odata.id alongside the
// full decoded JSON — matching real redfish_info.py's own per-category
// gate: `_find_chassis_resource()` hard-fails the whole category if no
// Chassis resource exists at all, but a problem with one specific
// command (e.g. no Fans present) only soft-fails that command's own
// entry in redfish_facts, never the module — see this file's own doc
// comment.
func redfishGetBareChassis(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (string, map[string]any, Result, error) {
	var chassis map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &chassis, "-1", "Chassis")
	if err != nil {
		return "", nil, Result{}, err
	}
	if r.RC != 0 {
		return "", nil, Fail("redfish_info: Chassis resource not found: " + redfishtoolErrMsg(r)), nil
	}
	uri, _ := chassis["@odata.id"].(string)
	return uri, chassis, Result{}, nil
}

// redfishChassisInventoryEntry copies exactly the properties real
// get_chassis_inventory itself reads (confirmed from its own source),
// each included only if present. Real Ansible returns one such entry
// per chassis in a flat list (self.chassis_uris); this port's own
// disclosed single-chassis scope (redfishtool's own --One default,
// the same narrowing already applied throughout this whole sub-batch)
// means the caller always wraps this in a one-element list.
func redfishChassisInventoryEntry(chassisData map[string]any) map[string]any {
	properties := []string{
		"Name", "Id", "ChassisType", "PartNumber", "AssetTag",
		"Manufacturer", "IndicatorLED", "SerialNumber", "Model",
	}
	entry := map[string]any{}
	for _, p := range properties {
		if v, ok := chassisData[p]; ok {
			entry[p] = v
		}
	}
	return entry
}

// redfishGetFanInventory reproduces real get_fan_inventory exactly for
// the single chassis this port already has in hand: if the chassis has
// no "Thermal" link at all, real Ansible's own loop just skips that
// chassis silently (no failure) — reproduced here as an empty,
// ret-less entries list. If Thermal exists but its own "Fans" property
// doesn't, real Ansible returns `{"ret": False, "msg": "No Fans
// present"}` — a soft, per-command failure embedded in redfish_facts,
// not a module failure (see this file's own doc comment), reproduced
// verbatim. Otherwise the 5 properties real get_fan_inventory itself
// reads are copied from each Fans[] entry.
func redfishGetFanInventory(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, chassisData map[string]any) (map[string]any, error) {
	thermal, ok := chassisData["Thermal"].(map[string]any)
	if !ok {
		return map[string]any{"entries": []any{}}, nil
	}
	thermalURI, _ := thermal["@odata.id"].(string)
	var data map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", thermalURI)
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return map[string]any{"ret": false, "msg": redfishtoolErrMsg(r)}, nil
	}
	fans, ok := data["Fans"].([]any)
	if !ok {
		return map[string]any{"ret": false, "msg": "No Fans present"}, nil
	}
	properties := []string{"Name", "FanName", "Reading", "ReadingUnits", "Status"}
	entries := []any{}
	for _, f := range fans {
		fanData, ok := f.(map[string]any)
		if !ok {
			continue
		}
		fan := map[string]any{}
		for _, p := range properties {
			if v, ok := fanData[p]; ok {
				fan[p] = v
			}
		}
		entries = append(entries, fan)
	}
	return map[string]any{"ret": true, "entries": entries}, nil
}

// redfishGetChassisPower reproduces real get_chassis_power exactly for
// the single chassis this port already has in hand: no "Power" link at
// all means real Ansible's own chassis_power_results list stays empty,
// returning the soft failure `{"ret": False, "msg": "Power information
// not found."}` — reproduced verbatim. If Power exists, real Ansible
// appends an entry (from PowerControl[0]'s properties, or an empty
// dict if PowerControl is itself missing/empty — still counted as
// "found", not a failure) rather than failing.
func redfishGetChassisPower(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, chassisData map[string]any) (map[string]any, error) {
	power, ok := chassisData["Power"].(map[string]any)
	if !ok {
		return map[string]any{"ret": false, "msg": "Power information not found."}, nil
	}
	powerURI, _ := power["@odata.id"].(string)
	var data map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", powerURI)
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return map[string]any{"ret": false, "msg": redfishtoolErrMsg(r)}, nil
	}
	entry := map[string]any{}
	if controls, ok := data["PowerControl"].([]any); ok && len(controls) > 0 {
		if first, ok := controls[0].(map[string]any); ok {
			properties := []string{
				"Name", "PowerAllocatedWatts", "PowerAvailableWatts", "PowerCapacityWatts",
				"PowerConsumedWatts", "PowerMetrics", "PowerRequestedWatts", "RelatedItem", "Status",
			}
			for _, p := range properties {
				if v, ok := first[p]; ok {
					entry[p] = v
				}
			}
		}
	}
	return map[string]any{"ret": true, "entries": []any{entry}}, nil
}

// redfishGetBareAccountService GETs the bare AccountService resource.
// Unlike Systems/Chassis/Managers, AccountService is a Redfish
// singleton (no Id-based collection of multiple instances) —
// confirmed from AccountServiceMain's own source, whose len(args)<2
// branch defaults straight to "get", never "collection" — so no "-1"
// is needed here, matching the AccountService "patch" usage already
// shipped in redfish_command.go. Doubles as the category-level
// existence gate (matching real `_find_accountservice_resource`) and,
// for GetAccountServiceConfig, is real get_accountservice_properties'
// entire raw return value verbatim — that real function does nothing
// more than this same GET.
func redfishGetBareAccountService(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (map[string]any, Result, error) {
	var svc map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &svc, "AccountService")
	if err != nil {
		return nil, Result{}, err
	}
	if r.RC != 0 {
		return nil, Fail("redfish_info: AccountService resource not found: " + redfishtoolErrMsg(r)), nil
	}
	return svc, Result{}, nil
}

// redfishListUsers reproduces real list_users: list the Accounts
// collection (`AccountService Accounts list`, the same redfishtool
// "list" idiom already proven for Sessions/Logs elsewhere in this
// batch — its own listCollection always includes each member's own
// "@odata.id"), GET each member's full resource, copy the 8
// properties real list_users itself reads, and filter out empty
// account slots exactly as real Ansible does: UserName=="" and not
// Enabled.
func redfishListUsers(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (map[string]any, error) {
	members, r, err := redfishListCollectionMembers(ctx, conn, baseuri, username, password, "AccountService", "Accounts", "list")
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return map[string]any{"ret": false, "msg": redfishtoolErrMsg(r)}, nil
	}
	properties := []string{"Id", "Name", "UserName", "RoleId", "Locked", "Enabled", "AccountTypes", "OEMAccountTypes"}
	entries := []any{}
	for _, uri := range members {
		var data map[string]any
		mr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", uri)
		if err != nil {
			return nil, err
		}
		if mr.RC != 0 {
			return map[string]any{"ret": false, "msg": redfishtoolErrMsg(mr)}, nil
		}
		user := map[string]any{}
		for _, p := range properties {
			if v, ok := data[p]; ok {
				user[p] = v
			}
		}
		userName, _ := user["UserName"].(string)
		enabled, _ := user["Enabled"].(bool)
		if userName == "" && !enabled {
			continue
		}
		entries = append(entries, user)
	}
	return map[string]any{"ret": true, "entries": entries}, nil
}

// redfishGetSessions reproduces real get_sessions: list the Sessions
// collection (`SessionService Sessions list`, the exact idiom
// redfish_command.go's own ClearSessions already uses), GET each
// member's full resource, and copy the 4 properties real get_sessions
// itself reads. A failure here (missing SessionService/Sessions
// collection entirely) is this category's only command, so it doubles
// as real redfish_info's own `_find_sessionservice_resource` hard-fail
// gate rather than a soft per-command embed.
func redfishGetSessions(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (map[string]any, Result, error) {
	members, r, err := redfishListCollectionMembers(ctx, conn, baseuri, username, password, "SessionService", "Sessions", "list")
	if err != nil {
		return nil, Result{}, err
	}
	if r.RC != 0 {
		return nil, Fail("redfish_info: SessionService/Sessions resource not found: " + redfishtoolErrMsg(r)), nil
	}
	properties := []string{"Description", "Id", "Name", "UserName"}
	entries := []any{}
	for _, uri := range members {
		var data map[string]any
		mr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", uri)
		if err != nil {
			return nil, Result{}, err
		}
		if mr.RC != 0 {
			return nil, Fail("redfish_info: GetSessions: " + redfishtoolErrMsg(mr)), nil
		}
		session := map[string]any{}
		for _, p := range properties {
			if v, ok := data[p]; ok {
				session[p] = v
			}
		}
		entries = append(entries, session)
	}
	return map[string]any{"ret": true, "entries": entries}, Result{}, nil
}

// redfishListCollectionMembers runs a redfishtool "list" operation
// (e.g. `AccountService Accounts list`, `SessionService Sessions
// list`) and returns each member's own "@odata.id" — the same shape
// already proven for redfish_command.go's ClearSessions/ClearLogs
// (redfishtool's own listCollection always includes "@odata.id" per
// member alongside "Id" and the requested prop, confirmed from its own
// source), extracted here into a shared helper now that a third and
// fourth real caller (ListUsers, GetSessions) need the identical
// list-then-walk shape.
func redfishListCollectionMembers(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, args ...string) ([]string, remoteexec.Result, error) {
	var coll struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &coll, args...)
	if err != nil {
		return nil, remoteexec.Result{}, err
	}
	if r.RC != 0 {
		return nil, r, nil
	}
	uris := make([]string, len(coll.Members))
	for i, m := range coll.Members {
		uris[i] = m.ODataID
	}
	return uris, r, nil
}
