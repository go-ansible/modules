package modules

import (
	"context"
	"strings"

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
//     GetPowerRestorePolicy, GetHealthReport, each reading the bare
//     `Systems` resource (redfishtool's own --One default — the
//     single-system case, same disclosed narrower-than-real-Ansible's-
//     multi-system aggregation this whole sub-batch already relies on)
//     and extracting exactly the properties real get_system_inventory/
//     get_boot_override/get_power_restore_policy/
//     get_system_health_report themselves read — confirmed field-by-
//     field against their own source, not guessed. Every one of these
//     four is real Ansible's own AGGREGATE-wrapped form
//     (`get_multi_system_inventory`/`get_multi_boot_override`/
//     `get_multi_power_restore_policy`/`get_multi_system_health_report`,
//     confirmed from redfish_info.py's own dispatch table — NOT the
//     bare non-aggregate function this port's own doc comments had
//     originally assumed), so the real output shape is TWO-level:
//     `{"ret": bool, "entries": [({"system_uri": uri}, {...})]}` — see
//     redfishAggregateOne's own doc comment for the real bug this
//     fixed in an earlier increment (the outer `{"ret":..,
//     "entries":..}` wrapper was missing entirely). GetBootOverride
//     additionally reproduces a real, easy-to-miss distinction: a
//     missing "Boot" key or a missing "BootSourceOverrideEnabled" key
//     are each their own real soft failure, but aggregate()'s own
//     source discards the inner "msg" entirely — both surface as a
//     bare `{"ret":false,"entries":[]}`, no message.
//   - Chassis: GetChassisInventory, GetFanInventory, GetChassisPower,
//     GetHealthReport, each reading the bare `Chassis` resource (same
//     --One single-chassis narrowing as Systems above) —
//     GetFanInventory/GetChassisPower each additionally discover and
//     GET their own Thermal/Power sub-resource, reproducing real
//     get_fan_inventory/get_chassis_power's exact soft-failure text
//     ("No Fans present", "Power information not found.") when the
//     expected link or property is missing, confirmed field-by-field
//     from their own source, not guessed. GetHealthReport is likewise
//     the real aggregate-wrapped form (`get_multi_chassis_health_report`
//     via `aggregate_chassis`, under its own "chassis_uri" key —
//     confirmed from `aggregate_chassis`'s own source, not assumed to
//     match Systems'/Manager's key names).
//   - Accounts: ListUsers (list `AccountService Accounts`, GET each
//     member, filter empty account slots exactly as real list_users
//     does: UserName=="" and not Enabled) and GetAccountServiceConfig
//     (the entire raw AccountService resource verbatim, since real
//     get_accountservice_properties does nothing more than that GET).
//   - Sessions: GetSessions (list `SessionService Sessions`, GET each
//     member) — this category's only command, so a missing
//     SessionService/Sessions resource is this port's own category-
//     level hard fail rather than a soft per-command embed.
//   - Update: GetFirmwareInventory/GetSoftwareInventory (list the
//     UpdateService's own FirmwareInventory/SoftwareInventory
//     collection, GET each member, 9-property whitelist confirmed from
//     `_software_inventory`'s own source — single-page only, a real
//     disclosed narrowing: real Ansible follows `Members@odata.
//     nextLink` pagination, this port does not) and
//     GetFirmwareUpdateCapabilities (the UpdateService resource's own
//     "Actions" dict, title-keyed, each action's
//     TransferProtocol@Redfish.AllowableValues — reproducing real
//     get_firmware_update_capabilities' exact two soft-failure
//     messages, "Key Actions not found."/"Actions list is empty.").
//     GetUpdateStatus is NOT wired: real get_update_status interprets
//     the raw HTTP status code (200/202/204/4xx) of a GET on an
//     arbitrary task/job handle to build its own status enum
//     (`_operation_results`) — redfishtool's own `raw` subcommand
//     prints only the JSON body in its default output mode, with no
//     way to recover the distinguishing HTTP status code, confirmed
//     from `raw.py`'s own source. A real, disclosed gap, not
//     approximated.
//   - Manager: all 8 real commands, completing this category.
//     GetManagerInventory (bare Manager resource, 11-property
//     whitelist, wrapped in the same one-element aggregate-tuple shape
//     as Systems/Chassis but under real Ansible's own "manager_uri"
//     key), GetNetworkProtocols (Managers' own NetworkProtocol named
//     subcommand, filtered to the same 14 known protocol-service names
//     `redfishNormalizeNetworkProtocols` in redfish_config.go already
//     validates against — reused, not redefined), GetServiceIdentification
//     (a real, disclosed EXCEPTION to this whole module's own soft-
//     fail convention: real get_service_identification calls
//     `module.fail_json` directly on a missing ServiceIdentification
//     property, confirmed from its own source — reproduced as a hard
//     Result{Failed:true}, not a soft embed, unlike every other command
//     in this file), and GetManagerNicInventory (list the Manager's own
//     EthernetInterfaces, GET each member, the same 14-property
//     `get_nic` whitelist real get_hostinterfaces also reuses,
//     wrapped under real Ansible's own "resource_uri" key). GetLogs
//     (find LogServices, list its members via the same `Managers Logs
//     list` idiom redfish_command.go's own ClearLogs uses, GET each to
//     find ITS OWN Entries sub-link, then GET each Entries collection
//     for its embedded LogEntry objects directly — no further per-entry
//     GET needed). GetVirtualMedia (find the Manager's own VirtualMedia
//     collection, GET each member, 10-property whitelist, wrapped under
//     "resource_uri" like GetManagerNicInventory). GetHostInterfaces
//     (find HostInterfaces, GET each member's 10-property whitelist,
//     plus its own embedded ManagerEthernetInterface/
//     HostEthernetInterfaces NIC lookups reusing get_nic's own
//     whitelist — real get_hostinterfaces' two nested NIC lookups), and
//     GetHealthReport — confirmed from its own source to have an EMPTY
//     subsystems list (unlike Systems/Chassis), reducing to just the
//     Manager's own top-level Status, wrapped under "manager_uri" via
//     `aggregate_managers`.
//
// GetHealthReport (Systems/Chassis/Manager) shares ONE real
// implementation (`get_health_report`, confirmed from its own source
// to be the single shared function behind all three
// `get_*_health_report` wrappers) — reproduced here as
// redfishGetHealthReport plus its own two recursive helpers
// (redfishGetHealthSubsystem/redfishGetHealthResource), handling all 3
// real subsystem-name shapes ("Links.X", "X.Y", and a bare name) and
// real Ansible's own "expanded" shortcut (a list item whose own
// "@odata.id" contains "#" and carries more than one key — common for
// PowerSupplies/Fans-style objects embedded directly in their parent
// resource — skips a redundant GET and uses the embedded object as-is).
//
// 4 more Chassis commands (GetChassisThermals, GetPsuInventory, and
// HPE-specific GetHPEThermalConfig/GetHPEFanPercentMin) and 10 more
// Systems commands (GetCpuInventory, GetMemoryInventory,
// GetNicInventory, GetStorageControllerInventory, GetDiskInventory,
// GetVolumeInventory, GetBiosAttributes, GetBootOrder, GetVirtualMedia,
// GetBiosRegistries) remain unwired — a later increment of this same
// batch.
//
// # A real bug a prior increment fixed
//
// redfishGetBareSystem (and redfishResourceSubURI in redfish_config.go,
// and the ResetToDefaults discovery in redfish_command.go) called a
// bare `Systems`/`Managers` subcommand with no "-1" flag. Confirmed
// from SystemsMain/ManagersMain/ChassisMain's own source: a bare call
// with no operation argument AND no ID-selecting option defaults to
// redfishtool's own "collection" operation, not "get" — so those calls
// were decoding a `{Members:[...]}` collection into the single-
// resource shape their callers expect. Fixed by adding "-1" to all
// three call sites (and to redfishGetBareChassis/redfishGetBareManager,
// both written with it from the start). AccountService/SessionService
// are unaffected: they're Redfish singletons (no Id-based collection),
// and their own Main functions default a bare call straight to "get" —
// confirmed separately.
//
// Args: category (required list); command (list, defaults per category
// when omitted); baseuri (required, real effect); username/password
// (real effect); manager (real effect, GetServiceIdentification only);
// auth_token (not supported, fails loud).
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
					facts["system"] = redfishAggregateOne("system_uri", systemURI, redfishSystemInventoryEntries(sysData))
				case "GetBootOverride":
					facts["boot_override"] = redfishGetBootOverride(systemURI, sysData)
				case "GetPowerRestorePolicy":
					facts["power_restore_policy"] = redfishAggregateOne("system_uri", systemURI, sysData["PowerRestorePolicy"])
				case "GetHealthReport":
					v, err := redfishGetSystemHealthReport(ctx, conn, baseuri, username, password, systemURI)
					if err != nil {
						return Result{}, err
					}
					facts["health_report"] = v
				}

			case "Chassis":
				chassisURI, chassisData, res, err := redfishGetBareChassis(ctx, conn, baseuri, username, password)
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
				case "GetHealthReport":
					v, err := redfishGetChassisHealthReport(ctx, conn, baseuri, username, password, chassisURI)
					if err != nil {
						return Result{}, err
					}
					facts["health_report"] = v
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

			case "Update":
				updateService, res, err := redfishGetBareUpdateService(ctx, conn, baseuri, username, password)
				if err != nil {
					return Result{}, err
				}
				if res.Failed {
					return res, nil
				}
				switch command {
				case "GetFirmwareInventory":
					v, err := redfishSoftwareOrFirmwareInventory(ctx, conn, baseuri, username, password, updateService, "FirmwareInventory", "No FirmwareInventory resource found")
					if err != nil {
						return Result{}, err
					}
					facts["firmware"] = v
				case "GetSoftwareInventory":
					v, err := redfishSoftwareOrFirmwareInventory(ctx, conn, baseuri, username, password, updateService, "SoftwareInventory", "No SoftwareInventory resource found")
					if err != nil {
						return Result{}, err
					}
					facts["software"] = v
				case "GetFirmwareUpdateCapabilities":
					facts["firmware_update_capabilities"] = redfishFirmwareUpdateCapabilities(updateService)
				}

			case "Manager":
				managerURI, mgrData, res, err := redfishGetBareManager(ctx, conn, baseuri, username, password)
				if err != nil {
					return Result{}, err
				}
				if res.Failed {
					return res, nil
				}
				switch command {
				case "GetManagerInventory":
					facts["manager"] = redfishAggregateOne("manager_uri", managerURI, redfishManagerInventoryEntries(mgrData))
				case "GetNetworkProtocols":
					v, err := redfishGetNetworkProtocols(ctx, conn, baseuri, username, password, mgrData)
					if err != nil {
						return Result{}, err
					}
					facts["network_protocols"] = v
				case "GetServiceIdentification":
					v, res, err := redfishGetServiceIdentification(ctx, conn, baseuri, username, password, mgrData, argString(args, "manager", ""))
					if err != nil {
						return Result{}, err
					}
					if res.Failed {
						return res, nil
					}
					facts["service_id"] = v
				case "GetManagerNicInventory":
					v, err := redfishGetManagerNicInventory(ctx, conn, baseuri, username, password, managerURI, mgrData)
					if err != nil {
						return Result{}, err
					}
					facts["manager_nics"] = v
				case "GetLogs":
					v, err := redfishGetLogs(ctx, conn, baseuri, username, password, mgrData)
					if err != nil {
						return Result{}, err
					}
					facts["log"] = v
				case "GetVirtualMedia":
					v, err := redfishGetVirtualMedia(ctx, conn, baseuri, username, password, managerURI, mgrData)
					if err != nil {
						return Result{}, err
					}
					facts["virtual_media"] = v
				case "GetHostInterfaces":
					v, err := redfishGetHostInterfaces(ctx, conn, baseuri, username, password, mgrData)
					if err != nil {
						return Result{}, err
					}
					facts["host_interfaces"] = v
				case "GetHealthReport":
					v, err := redfishGetManagerHealthReport(ctx, conn, baseuri, username, password, managerURI)
					if err != nil {
						return Result{}, err
					}
					facts["health_report"] = v
				}
			}
		}
	}

	return Ok("").WithExtra("redfish_facts", facts), nil
}

var redfishInfoCategories = map[string][]string{
	"Systems":  {"GetSystemInventory", "GetBootOverride", "GetPowerRestorePolicy", "GetHealthReport"},
	"Chassis":  {"GetChassisInventory", "GetFanInventory", "GetChassisPower", "GetHealthReport"},
	"Accounts": {"ListUsers", "GetAccountServiceConfig"},
	"Sessions": {"GetSessions"},
	"Update":   {"GetFirmwareInventory", "GetSoftwareInventory", "GetFirmwareUpdateCapabilities"},
	"Manager":  {"GetManagerInventory", "GetNetworkProtocols", "GetServiceIdentification", "GetManagerNicInventory", "GetLogs", "GetVirtualMedia", "GetHostInterfaces", "GetHealthReport"},
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

// redfishAggregateOne reproduces real Ansible's own shared `aggregate`
// helper exactly: `{"ret": bool, "entries": [({<key>: uri}, entries),
// ...]}` — a REAL bug in an earlier increment omitted this outer
// "ret"/"entries" wrapper entirely, returning just the bare tuple
// list. Confirmed by reading real redfish_info.py's own dispatch
// table: GetSystemInventory/GetBootOverride/GetPowerRestorePolicy call
// `get_multi_system_inventory`/`get_multi_boot_override`/
// `get_multi_power_restore_policy` — every one of them
// `aggregate_systems(...)`, not the bare non-aggregate function this
// port's own doc comments had assumed — and likewise
// GetManagerInventory/GetManagerNicInventory/GetVirtualMedia go
// through `get_multi_manager_inventory`/`get_multi_nic_inventory`/
// `get_multi_virtualmedia`, all aggregate-wrapped. Key differs per
// real caller: "system_uri" (aggregate_systems), "manager_uri"
// (aggregate_managers), "resource_uri" (get_multi_nic_inventory/
// get_multi_virtualmedia's own inline aggregation, not aggregate_*) —
// generalized from the Systems-only redfishAggregateOneSystem the
// moment Manager's own info commands needed the identical shape under
// different key names.
func redfishAggregateOne(key, uri string, entries any) map[string]any {
	return map[string]any{
		"ret": true,
		"entries": []any{
			[]any{map[string]any{key: uri}, entries},
		},
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

// redfishGetBootOverride reproduces real get_boot_override AND real
// get_multi_boot_override's own aggregate() wrapper exactly, including
// a real, easy-to-miss distinction: a MISSING "Boot" key or a MISSING
// "BootSourceOverrideEnabled" key are each their own real soft failure
// ("Key Boot not found." / "No boot override is enabled.") — but
// aggregate()'s own source only propagates the inner "ret" flag, never
// the inner "msg", into its own `{"ret":.., "entries":[...]}` result
// (it only appends to "entries" when the inner call's own dict HAS an
// "entries" key at all) — so both real soft failures surface here as
// bare `{"ret": false, "entries": []}`, no message, confirmed from
// both functions' own source rather than assumed to preserve one.
// An EXPLICIT `BootSourceOverrideEnabled: false` (as opposed to the
// key being absent) is different again: real Ansible treats that as
// a genuine SUCCESS with empty entries, not a failure — the boolean
// value `false` is real Ansible's own `is not False` check failing,
// which simply skips populating any properties, not an error path.
func redfishGetBootOverride(systemURI string, sysData map[string]any) map[string]any {
	boot, ok := sysData["Boot"].(map[string]any)
	if !ok {
		return map[string]any{"ret": false, "entries": []any{}}
	}
	enabled, hasEnabled := boot["BootSourceOverrideEnabled"]
	if !hasEnabled {
		return map[string]any{"ret": false, "entries": []any{}}
	}
	overrides := map[string]any{}
	if enabled == false {
		return redfishAggregateOne("system_uri", systemURI, overrides)
	}
	properties := []string{
		"BootSourceOverrideEnabled", "BootSourceOverrideTarget", "BootSourceOverrideMode",
		"UefiTargetBootSourceOverride", "BootSourceOverrideTarget@Redfish.AllowableValues",
	}
	for _, p := range properties {
		if v, ok := boot[p]; ok && v != nil {
			overrides[p] = v
		}
	}
	return redfishAggregateOne("system_uri", systemURI, overrides)
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

// redfishGetBareUpdateService discovers and GETs the UpdateService
// resource: `redfishtool root` (the ServiceRoot resource, the same
// discovery hop redfish_command.go's own SimpleUpdate already uses,
// since redfishtool has no named UpdateService subcommand at all —
// confirmed absent from its own subcommand list), find the
// "UpdateService" link, GET it. Doubles as the category-level
// existence gate (matching real `_find_updateservice_resource`); the
// returned map already carries "FirmwareInventory"/"SoftwareInventory"
// sub-links and "Actions" directly, exactly what real
// `_find_updateservice_resource` itself reads before any command runs.
func redfishGetBareUpdateService(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (map[string]any, Result, error) {
	var root map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &root, "root")
	if err != nil {
		return nil, Result{}, err
	}
	if r.RC != 0 {
		return nil, Fail("redfish_info: UpdateService resource not found: " + redfishtoolErrMsg(r)), nil
	}
	link, ok := root["UpdateService"].(map[string]any)
	if !ok {
		return nil, Fail("redfish_info: UpdateService resource not found"), nil
	}
	uri, _ := link["@odata.id"].(string)
	var svc map[string]any
	r2, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &svc, "raw", "GET", uri)
	if err != nil {
		return nil, Result{}, err
	}
	if r2.RC != 0 {
		return nil, Fail("redfish_info: UpdateService resource not found: " + redfishtoolErrMsg(r2)), nil
	}
	return svc, Result{}, nil
}

// redfishSoftwareOrFirmwareInventory reproduces real
// `_software_inventory` (shared by get_firmware_inventory and
// get_software_inventory) for the collection linked from
// updateService[collKey]: list its members (single page only — a real,
// disclosed narrowing, see this file's own doc comment), GET each, and
// copy the 9 properties real `_software_inventory` itself reads.
func redfishSoftwareOrFirmwareInventory(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, updateService map[string]any, collKey, missingMsg string) (map[string]any, error) {
	coll, ok := updateService[collKey].(map[string]any)
	if !ok {
		return map[string]any{"ret": false, "msg": missingMsg}, nil
	}
	uri, _ := coll["@odata.id"].(string)
	members, r, err := redfishListCollectionMembers(ctx, conn, baseuri, username, password, "raw", "GET", uri)
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return map[string]any{"ret": false, "msg": redfishtoolErrMsg(r)}, nil
	}
	properties := []string{
		"Name", "Id", "Status", "Version", "Updateable",
		"SoftwareId", "LowestSupportedVersion", "Manufacturer", "ReleaseDate",
	}
	entries := []any{}
	for _, memberURI := range members {
		var data map[string]any
		mr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", memberURI)
		if err != nil {
			return nil, err
		}
		if mr.RC != 0 {
			return map[string]any{"ret": false, "msg": redfishtoolErrMsg(mr)}, nil
		}
		entry := map[string]any{}
		for _, p := range properties {
			if v, ok := data[p]; ok {
				entry[p] = v
			}
		}
		entries = append(entries, entry)
	}
	return map[string]any{"ret": true, "entries": entries}, nil
}

// redfishFirmwareUpdateCapabilities reproduces real
// get_firmware_update_capabilities exactly: "MultipartHttpPushUri"'s
// mere presence (not its value) sets multipart_supported; each entry
// under the UpdateService's own "Actions" dict becomes one output
// entry keyed by that action's "title" (falling back to the action's
// own dict key when absent), valued by its
// "TransferProtocol@Redfish.AllowableValues" (falling back to real
// Ansible's own exact placeholder string when absent) — reproducing
// its exact two soft-failure messages when Actions is missing or
// empty.
func redfishFirmwareUpdateCapabilities(updateService map[string]any) map[string]any {
	_, multipartSupported := updateService["MultipartHttpPushUri"]
	actions, ok := updateService["Actions"].(map[string]any)
	if !ok {
		return map[string]any{"ret": false, "msg": "Key Actions not found."}
	}
	if len(actions) == 0 {
		return map[string]any{"ret": false, "msg": "Actions list is empty."}
	}
	entries := map[string]any{}
	for key, raw := range actions {
		action, _ := raw.(map[string]any)
		title, ok := action["title"].(string)
		if !ok || title == "" {
			title = key
		}
		allowable, ok := action["TransferProtocol@Redfish.AllowableValues"]
		if !ok {
			allowable = []any{"Key TransferProtocol@Redfish.AllowableValues not found"}
		}
		entries[title] = allowable
	}
	return map[string]any{"ret": true, "entries": entries, "multipart_supported": multipartSupported}
}

// redfishGetBareManager GETs the bare Manager resource ("-1" required
// for the same reason documented on redfishGetBareSystem/
// redfishGetBareChassis) and returns its own @odata.id alongside the
// full decoded JSON — matching real redfish_info.py's own per-category
// gate: `_find_managers_resource` hard-fails the whole category if no
// Manager resource exists at all.
func redfishGetBareManager(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string) (string, map[string]any, Result, error) {
	var mgr map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &mgr, "-1", "Managers")
	if err != nil {
		return "", nil, Result{}, err
	}
	if r.RC != 0 {
		return "", nil, Fail("redfish_info: Managers resource not found: " + redfishtoolErrMsg(r)), nil
	}
	uri, _ := mgr["@odata.id"].(string)
	return uri, mgr, Result{}, nil
}

// redfishManagerInventoryEntries copies exactly the properties real
// get_manager_inventory itself reads (confirmed from its own source),
// each included only if present.
func redfishManagerInventoryEntries(mgrData map[string]any) map[string]any {
	properties := []string{
		"Id", "FirmwareVersion", "ManagerType", "Manufacturer", "Model",
		"PartNumber", "PowerState", "SerialNumber", "ServiceIdentification",
		"Status", "UUID",
	}
	entries := map[string]any{}
	for _, p := range properties {
		if v, ok := mgrData[p]; ok {
			entries[p] = v
		}
	}
	return entries
}

// redfishGetNetworkProtocols reproduces real get_network_protocols:
// discover the Manager's own "NetworkProtocol" link (redfishtool has a
// NAMED subcommand for it, `Managers NetworkProtocol`, already used by
// redfish_config.go's own SetNetworkProtocols discovery), GET it, and
// keep only the same 14 known protocol-service names
// `redfishNetworkProtocolServices` (redfish_config.go) already
// validates SetNetworkProtocols input against — reused verbatim rather
// than redefining the same real list twice.
func redfishGetNetworkProtocols(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, mgrData map[string]any) (map[string]any, error) {
	link, ok := mgrData["NetworkProtocol"].(map[string]any)
	if !ok {
		return map[string]any{"ret": false, "msg": "NetworkProtocol resource not found"}, nil
	}
	uri, _ := link["@odata.id"].(string)
	var data map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", uri)
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return map[string]any{"ret": false, "msg": redfishtoolErrMsg(r)}, nil
	}
	entries := map[string]any{}
	for name, v := range data {
		if redfishNetworkProtocolServices[name] {
			entries[name] = v
		}
	}
	return map[string]any{"ret": true, "entries": entries}, nil
}

// redfishGetServiceIdentification reproduces real
// get_service_identification for this port's single-manager scope: the
// real function resolves an omitted `manager` argument from the sole
// discovered manager's own Id when there is exactly one manager (this
// port's own permanent scope, so that always applies) and GETs a
// hardcoded absolute path `/redfish/v1/Managers/<id>` directly rather
// than the already-discovered manager URI — confirmed from its own
// source, not assumed to reuse mgrData's own resource. A missing
// "ServiceIdentification" property is a REAL, DISCLOSED EXCEPTION to
// this whole file's own soft-fail convention: real
// get_service_identification calls `module.fail_json` directly rather
// than returning `{"ret": False, ...}` like every sibling get_* — this
// port reproduces that as a genuine Result{Failed:true}, not a soft
// embed.
func redfishGetServiceIdentification(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, mgrData map[string]any, manager string) (map[string]any, Result, error) {
	if manager == "" {
		id, ok := mgrData["Id"].(string)
		if !ok || id == "" {
			return nil, Fail("redfish_info: GetServiceIdentification: could not determine the manager identity"), nil
		}
		manager = id
	}
	var data map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", "/redfish/v1/Managers/"+manager)
	if err != nil {
		return nil, Result{}, err
	}
	if r.RC != 0 {
		return nil, Fail("redfish_info: GetServiceIdentification: " + redfishtoolErrMsg(r)), nil
	}
	serviceID, ok := data["ServiceIdentification"]
	if !ok {
		return nil, Fail("redfish_info: GetServiceIdentification: Service ID not found for manager " + manager), nil
	}
	return map[string]any{"ret": true, "service_identification": serviceID}, Result{}, nil
}

// redfishNicEntries copies exactly the properties real get_nic itself
// reads (confirmed from its own source, and reused unchanged by real
// get_hostinterfaces for its own embedded NIC lookups — GetHostInterfaces
// itself remains unwired this increment, see this file's own doc
// comment, but the same whitelist is captured here as its own named
// function against the day that command is picked up).
func redfishNicEntries(data map[string]any) map[string]any {
	properties := []string{
		"Name", "Id", "Description", "FQDN", "IPv4Addresses", "IPv6Addresses",
		"NameServers", "MACAddress", "PermanentMACAddress", "SpeedMbps",
		"MTUSize", "AutoNeg", "Status", "LinkStatus",
	}
	entries := map[string]any{}
	for _, p := range properties {
		if v, ok := data[p]; ok {
			entries[p] = v
		}
	}
	return entries
}

// redfishGetManagerNicInventory reproduces real get_nic_inventory for
// the Manager's own EthernetInterfaces: list the collection (redfishtool's
// own `Managers EthernetInterfaces list`), GET each member, whitelist via
// redfishNicEntries, wrapped in the same one-element aggregate-tuple
// shape real get_multi_nic_inventory itself produces under its own
// "resource_uri" key (confirmed from its own source — a different key
// than GetManagerInventory's "manager_uri", not a typo).
func redfishGetManagerNicInventory(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, managerURI string, mgrData map[string]any) (map[string]any, error) {
	link, ok := mgrData["EthernetInterfaces"].(map[string]any)
	if !ok {
		return redfishAggregateOne("resource_uri", managerURI, []any{}), nil
	}
	uri, _ := link["@odata.id"].(string)
	members, r, err := redfishListCollectionMembers(ctx, conn, baseuri, username, password, "raw", "GET", uri)
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return redfishAggregateOne("resource_uri", managerURI, []any{}), nil
	}
	entries := []any{}
	for _, memberURI := range members {
		var data map[string]any
		mr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", memberURI)
		if err != nil {
			return nil, err
		}
		if mr.RC != 0 {
			continue
		}
		entries = append(entries, redfishNicEntries(data))
	}
	return redfishAggregateOne("resource_uri", managerURI, entries), nil
}

// redfishGetLogs reproduces real get_logs exactly: find the Manager's
// own "LogServices" link, list its members (`Managers Logs list`, the
// same idiom redfish_command.go's own ClearLogs already uses), GET
// each member to find ITS OWN "Entries" sub-link (skipped silently if
// absent — real Ansible's own behavior, not a failure), then GET each
// Entries collection and copy the 7 properties real get_logs itself
// reads from each embedded LogEntry directly (no further per-entry GET
// needed — the Entries collection's own Members already carry the full
// LogEntry objects). The output log name is the Entries URI's own last
// path segment, confirmed from real get_logs' own `rstrip("/").
// split("/")[-1]`.
func redfishGetLogs(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, mgrData map[string]any) (map[string]any, error) {
	if _, ok := mgrData["LogServices"]; !ok {
		return map[string]any{"ret": false, "msg": "LogServices resource not found"}, nil
	}
	members, r, err := redfishListCollectionMembers(ctx, conn, baseuri, username, password, "Managers", "Logs", "list")
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return map[string]any{"ret": false, "msg": redfishtoolErrMsg(r)}, nil
	}
	properties := []string{"Severity", "Created", "EntryType", "OemRecordFormat", "Message", "MessageId", "MessageArgs"}
	logs := []any{}
	for _, logSvcURI := range members {
		var logSvc map[string]any
		lr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &logSvc, "raw", "GET", logSvcURI)
		if err != nil {
			return nil, err
		}
		if lr.RC != 0 {
			return map[string]any{"ret": false, "msg": redfishtoolErrMsg(lr)}, nil
		}
		entriesLink, ok := logSvc["Entries"].(map[string]any)
		if !ok {
			continue
		}
		entriesURI, _ := entriesLink["@odata.id"].(string)
		var entriesData map[string]any
		er, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &entriesData, "raw", "GET", entriesURI)
		if err != nil {
			return nil, err
		}
		if er.RC != 0 {
			return map[string]any{"ret": false, "msg": redfishtoolErrMsg(er)}, nil
		}
		description, ok := entriesData["Description"].(string)
		if !ok {
			description = "Collection of log entries"
		}
		logEntries := []any{}
		if rawMembers, ok := entriesData["Members"].([]any); ok {
			for _, m := range rawMembers {
				logEntry, ok := m.(map[string]any)
				if !ok {
					continue
				}
				entry := map[string]any{}
				for _, p := range properties {
					if v, ok := logEntry[p]; ok {
						entry[p] = v
					}
				}
				if len(entry) > 0 {
					logEntries = append(logEntries, entry)
				}
			}
		}
		logName := strings.TrimRight(entriesURI, "/")
		if idx := strings.LastIndex(logName, "/"); idx >= 0 {
			logName = logName[idx+1:]
		}
		logs = append(logs, map[string]any{"Description": description, logName: logEntries})
	}
	return map[string]any{"ret": true, "entries": logs}, nil
}

// redfishGetVirtualMedia reproduces real get_virtualmedia/
// get_multi_virtualmedia for the Manager's own VirtualMedia collection:
// find the "VirtualMedia" link, list its members, GET each, copy the
// 10 properties real get_virtualmedia itself reads, wrapped under real
// Ansible's own "resource_uri" key (get_multi_virtualmedia's own tuple
// shape — the same shape and key GetManagerNicInventory already uses).
func redfishGetVirtualMedia(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, managerURI string, mgrData map[string]any) (map[string]any, error) {
	link, ok := mgrData["VirtualMedia"].(map[string]any)
	if !ok {
		return redfishAggregateOne("resource_uri", managerURI, []any{}), nil
	}
	uri, _ := link["@odata.id"].(string)
	members, r, err := redfishListCollectionMembers(ctx, conn, baseuri, username, password, "raw", "GET", uri)
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return redfishAggregateOne("resource_uri", managerURI, []any{}), nil
	}
	properties := []string{
		"Description", "ConnectedVia", "Id", "MediaTypes", "Image",
		"ImageName", "Name", "WriteProtected", "TransferMethod", "TransferProtocolType",
	}
	entries := []any{}
	for _, memberURI := range members {
		var data map[string]any
		mr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", memberURI)
		if err != nil {
			return nil, err
		}
		if mr.RC != 0 {
			continue
		}
		entry := map[string]any{}
		for _, p := range properties {
			if v, ok := data[p]; ok {
				entry[p] = v
			}
		}
		entries = append(entries, entry)
	}
	return redfishAggregateOne("resource_uri", managerURI, entries), nil
}

// redfishGetHostInterfaces reproduces real get_hostinterfaces for this
// port's single-manager scope: find the Manager's own "HostInterfaces"
// link (absent means no HostInterface objects at all — a real, single
// soft-failure message confirmed from source, not per-manager since
// this port only ever has one), GET the collection, GET each member,
// copy the 10 properties real get_hostinterfaces itself reads (only
// when the value is non-nil, confirmed from its own extra `is not
// None` check — a real, easy-to-miss detail beyond a plain "in data"
// test), then — matching real get_hostinterfaces' own two embedded NIC
// lookups, both reusing get_nic's exact whitelist
// (redfishNicEntries) — attach "ManagerEthernetInterface" (a single
// link) and "HostEthernetInterfaces" (a collection of links) when
// present.
func redfishGetHostInterfaces(ctx context.Context, conn remoteexec.Connection, baseuri, username, password string, mgrData map[string]any) (map[string]any, error) {
	link, ok := mgrData["HostInterfaces"].(map[string]any)
	if !ok {
		return map[string]any{"ret": false, "msg": "No HostInterface objects found"}, nil
	}
	uri, _ := link["@odata.id"].(string)
	members, r, err := redfishListCollectionMembers(ctx, conn, baseuri, username, password, "raw", "GET", uri)
	if err != nil {
		return nil, err
	}
	if r.RC != 0 || len(members) == 0 {
		return map[string]any{"ret": false, "msg": "No HostInterface objects found"}, nil
	}
	properties := []string{
		"Id", "Name", "Description", "HostInterfaceType", "Status", "InterfaceEnabled",
		"ExternallyAccessible", "AuthenticationModes", "AuthNoneRoleId", "CredentialBootstrapping",
	}
	entries := []any{}
	for _, memberURI := range members {
		var data map[string]any
		mr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", memberURI)
		if err != nil {
			return nil, err
		}
		if mr.RC != 0 {
			continue
		}
		entry := map[string]any{}
		for _, p := range properties {
			if v, ok := data[p]; ok && v != nil {
				entry[p] = v
			}
		}
		if mei, ok := data["ManagerEthernetInterface"].(map[string]any); ok {
			if meiURI, ok := mei["@odata.id"].(string); ok {
				var nicData map[string]any
				nr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &nicData, "raw", "GET", meiURI)
				if err != nil {
					return nil, err
				}
				if nr.RC == 0 {
					entry["ManagerEthernetInterface"] = redfishNicEntries(nicData)
				}
			}
		}
		if hei, ok := data["HostEthernetInterfaces"].(map[string]any); ok {
			if heiURI, ok := hei["@odata.id"].(string); ok {
				nicURIs, hr, err := redfishListCollectionMembers(ctx, conn, baseuri, username, password, "raw", "GET", heiURI)
				if err != nil {
					return nil, err
				}
				if hr.RC == 0 {
					hostNics := []any{}
					for _, nicURI := range nicURIs {
						var nicData map[string]any
						nr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &nicData, "raw", "GET", nicURI)
						if err != nil {
							return nil, err
						}
						if nr.RC != 0 {
							continue
						}
						hostNics = append(hostNics, redfishNicEntries(nicData))
					}
					entry["HostEthernetInterfaces"] = hostNics
				}
			}
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return map[string]any{"ret": false, "msg": "No HostInterface objects found"}, nil
	}
	return map[string]any{"ret": true, "entries": entries}, nil
}

// redfishToSingular reproduces real to_singular exactly: a name ending
// in "ies" becomes "...y" (e.g. "PowerSupplies" -> "PowerSupply"), a
// name ending in a plain "s" drops it (e.g. "Fans" -> "Fan",
// "EthernetInterfaces" -> "EthernetInterface"), anything else is
// returned unchanged (e.g. "Memory", "Storage" both already end in a
// vowel, not "s").
func redfishToSingular(name string) string {
	if strings.HasSuffix(name, "ies") {
		return name[:len(name)-3] + "y"
	}
	if strings.HasSuffix(name, "s") {
		return name[:len(name)-1]
	}
	return name
}

// redfishGetHealthResource reproduces real get_health_resource: if
// `expanded` is non-nil, use it directly instead of a fresh GET — real
// Ansible's own shortcut for a list item whose own "@odata.id" already
// contains "#" (a JSON-pointer-style fragment reference, common for
// PowerSupplies/Fans-style objects embedded directly inside their
// parent resource rather than addressable as their own separate
// resource) AND carries more than just that one key — otherwise GET
// uri fresh. If the resulting data is itself a collection (has
// "Members"), walk each member and append one `{<singular>_uri:
// memberURI, "Status": ...}` entry per member; otherwise append a
// single such entry for the resource itself (using `uri` as its own
// `_uri`, even when that uri is a fragment reference — matching real
// Ansible's own behavior exactly, not adjusted for readability). A
// transport error propagates; an HTTP-level failure (RC!=0) is
// silently skipped, matching real Ansible's own `if r.get(ret): ...
// else: return` (no failure surfaced at this level at all).
func redfishGetHealthResource(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, subsystemKey, uri string, expanded, health map[string]any) error {
	d := expanded
	if d == nil {
		r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &d, "raw", "GET", uri)
		if err != nil {
			return err
		}
		if r.RC != 0 {
			return nil
		}
	}
	singular := redfishToSingular(strings.ToLower(subsystemKey))
	list, _ := health[subsystemKey].([]any)
	if members, ok := d["Members"].([]any); ok {
		for _, m := range members {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			u, _ := mm["@odata.id"].(string)
			if u == "" {
				continue
			}
			var p map[string]any
			pr, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &p, "raw", "GET", u)
			if err != nil {
				return err
			}
			if pr.RC != 0 {
				continue
			}
			status, ok := p["Status"]
			if !ok {
				status = "Status not available"
			}
			list = append(list, map[string]any{singular + "_uri": u, "Status": status})
		}
	} else {
		status, ok := d["Status"]
		if !ok {
			status = "Status not available"
		}
		list = append(list, map[string]any{singular + "_uri": uri, "Status": status})
	}
	health[subsystemKey] = list
	return nil
}

// redfishGetHealthSubsystem reproduces real get_health_subsystem
// exactly, including its own recursive "Members" fallback: if
// `subsystem` isn't a direct key on `data` but `data` is itself a
// collection (e.g. "NetworkInterfaces.NetworkPorts" — NetworkInterfaces
// is a COLLECTION of individual NetworkInterface resources, so
// "NetworkPorts" must instead be searched inside EACH member), walk
// each member's own data and recurse.
func redfishGetHealthSubsystem(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, subsystem string, data map[string]any, health map[string]any) error {
	if sub, ok := data[subsystem]; ok {
		switch v := sub.(type) {
		case []any:
			for _, item := range v {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				uri, ok := m["@odata.id"].(string)
				if !ok || uri == "" {
					continue
				}
				var expanded map[string]any
				if strings.Contains(uri, "#") && len(m) > 1 {
					expanded = m
				}
				if err := redfishGetHealthResource(ctx, conn, baseuri, username, password, subsystem, uri, expanded, health); err != nil {
					return err
				}
			}
		case map[string]any:
			if uri, ok := v["@odata.id"].(string); ok {
				if err := redfishGetHealthResource(ctx, conn, baseuri, username, password, subsystem, uri, nil, health); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if members, ok := data["Members"].([]any); ok {
		for _, m := range members {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			u, _ := mm["@odata.id"].(string)
			if u == "" {
				continue
			}
			var d map[string]any
			r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &d, "raw", "GET", u)
			if err != nil {
				return err
			}
			if r.RC != 0 {
				continue
			}
			if err := redfishGetHealthSubsystem(ctx, conn, baseuri, username, password, subsystem, d, health); err != nil {
				return err
			}
		}
	}
	return nil
}

// redfishGetHealthReport reproduces real get_health_report exactly:
// GET the top-level resource, record its own top-level Status under
// `category`, then for each subsystem name in `subsystems` — handling
// all 3 real naming shapes ("Links.X" reads from the resource's own
// "Links" object; "X.Y" GETs the resource's own "X" sub-link first,
// then searches for "Y" there; a bare name searches the top-level
// resource's own data directly) — call redfishGetHealthSubsystem and
// drop the subsystem's own key entirely if it ends up empty (real
// Ansible's own `if not health[sub]: del health[sub]`).
func redfishGetHealthReport(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, category, uri string, subsystems []string) (map[string]any, error) {
	var data map[string]any
	r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &data, "raw", "GET", uri)
	if err != nil {
		return nil, err
	}
	if r.RC != 0 {
		return map[string]any{"ret": false, "msg": redfishtoolErrMsg(r)}, nil
	}
	health := map[string]any{}
	status, ok := data["Status"]
	if !ok {
		status = "Status not available"
	}
	health[category] = map[string]any{"Status": status}
	for _, sub := range subsystems {
		var d map[string]any
		subKey := sub
		switch {
		case strings.HasPrefix(sub, "Links."):
			subKey = strings.TrimPrefix(sub, "Links.")
			links, _ := data["Links"].(map[string]any)
			if links == nil {
				links = map[string]any{}
			}
			d = links
		case strings.Contains(sub, "."):
			parts := strings.SplitN(sub, ".", 2)
			p, s := parts[0], parts[1]
			subKey = s
			link, ok := data[p].(map[string]any)
			if !ok {
				continue
			}
			u, ok := link["@odata.id"].(string)
			if !ok || u == "" {
				continue
			}
			var sd map[string]any
			r2, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &sd, "raw", "GET", u)
			if err != nil {
				return nil, err
			}
			if r2.RC != 0 {
				continue
			}
			d = sd
		default:
			d = data
		}
		health[subKey] = []any{}
		if err := redfishGetHealthSubsystem(ctx, conn, baseuri, username, password, subKey, d, health); err != nil {
			return nil, err
		}
		if list, ok := health[subKey].([]any); ok && len(list) == 0 {
			delete(health, subKey)
		}
	}
	return map[string]any{"ret": true, "entries": health}, nil
}

// redfishWrapMultiHealthReport reproduces real get_multi_*_health_report's
// own aggregate() wrapping around a SINGLE resource's
// get_health_report result — confirmed from aggregate()'s own source:
// it pops the inner "ret", and only appends `({key: uri}, inner
// "entries")` to its own "entries" list when the inner result actually
// HAS an "entries" key at all (a transport-level failure returns
// early with no "entries" key, so aggregate() silently contributes
// NOTHING for that member — same lossy "message discarded, just
// ret:false + empty entries" pattern already confirmed for
// GetBootOverride's own aggregate wrap).
func redfishWrapMultiHealthReport(key, uri string, inner map[string]any) map[string]any {
	entries, ok := inner["entries"]
	if !ok {
		return map[string]any{"ret": false, "entries": []any{}}
	}
	return redfishAggregateOne(key, uri, entries)
}

// redfishGetSystemHealthReport implements real get_system_health_report's
// own exact 7-subsystem list, confirmed from its own source, wrapped
// exactly as real get_multi_system_health_report's own
// aggregate_systems call does.
func redfishGetSystemHealthReport(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, systemURI string) (map[string]any, error) {
	subsystems := []string{
		"Processors", "Memory", "SimpleStorage", "Storage", "EthernetInterfaces",
		"NetworkInterfaces.NetworkPorts", "NetworkInterfaces.NetworkDeviceFunctions",
	}
	inner, err := redfishGetHealthReport(ctx, conn, baseuri, username, password, "System", systemURI, subsystems)
	if err != nil {
		return nil, err
	}
	return redfishWrapMultiHealthReport("system_uri", systemURI, inner), nil
}

// redfishGetChassisHealthReport implements real get_chassis_health_report's
// own exact 3-subsystem list, confirmed from its own source, wrapped
// exactly as real get_multi_chassis_health_report's own
// aggregate_chassis call does — under "chassis_uri", confirmed from
// aggregate_chassis's own source, not assumed to match Systems/Manager.
func redfishGetChassisHealthReport(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, chassisURI string) (map[string]any, error) {
	subsystems := []string{"Power.PowerSupplies", "Thermal.Fans", "Links.PCIeDevices"}
	inner, err := redfishGetHealthReport(ctx, conn, baseuri, username, password, "Chassis", chassisURI, subsystems)
	if err != nil {
		return nil, err
	}
	return redfishWrapMultiHealthReport("chassis_uri", chassisURI, inner), nil
}

// redfishGetManagerHealthReport implements real
// get_manager_health_report — confirmed from its own source to have
// an EMPTY subsystems list (unlike Systems/Chassis), so this reduces
// to just the Manager's own top-level Status — wrapped exactly as real
// get_multi_manager_health_report's own aggregate_managers call does.
func redfishGetManagerHealthReport(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, managerURI string) (map[string]any, error) {
	inner, err := redfishGetHealthReport(ctx, conn, baseuri, username, password, "Manager", managerURI, nil)
	if err != nil {
		return nil, err
	}
	return redfishWrapMultiHealthReport("manager_uri", managerURI, inner), nil
}
