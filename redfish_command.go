package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRedfishCommand implements Ansible's `redfish_command` module —
// the vendor-NEUTRAL Redfish command module, unlike this batch's
// vendor-specific idrac_redfish_command/ilo_redfish_command/
// xcc_redfish_command. Substitutes DMTF's own `redfishtool`, run in
// genuine remote mode (see redfishtool_common.go's own doc comment).
//
// # This increment's real scope
//
// This is a multi-session batch (real redfish_command.py has 6
// categories and ~35 declared commands across them); this increment
// covers the highest-value, most commonly automated ones, each mapped
// to a real redfishtool operation confirmed against redfishtool's own
// source (Systems.py/Chassis.py) before writing this file — not
// guessed:
//
//   - Systems power: PowerOn/PowerForceOff/PowerForceRestart/
//     PowerGracefulRestart/PowerGracefulShutdown/PowerReboot/PowerCycle
//     via `redfishtool Systems reset <resetType>` — real Ansible strips
//     the "Power" prefix (PowerCycle is its own declared exception, kept
//     whole) and maps "Reboot"->"GracefulRestart"; redfishtool's own
//     reset op accepts exactly those resetType strings (confirmed from
//     its validResetTypes list). PowerFullPowerCycle has NO redfishtool
//     equivalent (absent from that same list) — not in this category's
//     command list below, so it fails loud via the standard
//     redfishCheckCommands path rather than a special-cased message.
//   - Systems boot override: SetOneTimeBoot/EnableContinuousBootOverride/
//     DisableBootOverride via `redfishtool Systems setBootOverride
//     <enabledVal> [<targetVal>]` — see redfishSetBootOverride's own doc
//     comment for the bootdevice mapping and two disclosed gaps
//     (UefiTarget/UefiBootNext, boot_override_mode).
//   - Indicator LED, Systems and Chassis: IndicatorLedOn/Off/Blink via
//     `redfishtool Systems|Chassis setIndicatorLed <Lit|Off|Blinking>`.
//   - Sessions: ClearSessions/CreateSession/DeleteSession via
//     `redfishtool SessionService login|logout` — see
//     redfishSessionCommand's own doc comment.
//   - Accounts: AddUser/DeleteUser/EnableUser/DisableUser/
//     UpdateUserRole/UpdateUserPassword/UpdateAccountServiceProperties
//     via `redfishtool AccountService adduser|deleteuser|useradmin|
//     setpassword|patch` — see redfishAccountCommand's own doc comment
//     for real idempotency handling and two disclosed gaps
//     (UpdateUserName, UpdateUserAccountTypes).
//   - Manager: PowerOn/PowerForceOff/PowerForceRestart/
//     PowerGracefulRestart/PowerGracefulShutdown/PowerReboot/
//     GracefulRestart (a legacy alias real Ansible maps onto
//     PowerGracefulRestart) via `redfishtool Managers reset <resetType>`
//     — real Manager Power* deliberately excludes PowerCycle/
//     PowerFullPowerCycle, unlike Systems (confirmed from real
//     redfish_command.py's own CATEGORY_COMMANDS_ALL, not assumed to
//     mirror Systems); ClearLogs via `redfishtool Managers Logs list`
//     then `clearLog <id>` per entry — see redfishManagerCommand's own
//     doc comment for the wait/wait_timeout gap.
//
// Update, VirtualMediaInsert/Eject, ResetToDefaults, and
// VerifyBiosAttributes are declared with empty command lists (or absent
// entirely from Manager's own list, for the latter three) below — real
// categories/commands, none wired yet — a later increment of this same
// batch, not assumed to work.
//
// Args: category (required); command (required list); baseuri
// (required, real effect); username/password (real effect, staged via
// a temp cfgFile, never argv); auth_token (not supported, fails loud —
// see redfishtool_common.go's own doc comment); bootdevice; uefi_target;
// boot_next; boot_override_mode; session_uri; account_username;
// account_password; account_roleid; account_updatename;
// account_properties.
func moduleRedfishCommand(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	commands := argStringList(args, "command")
	if len(commands) == 0 {
		return Result{}, errArg("redfish_command: missing required argument: command")
	}
	if res, ok := redfishCheckCategory("redfish_command", category, redfishCommandCategories); !ok {
		return res, nil
	}
	if res, ok := redfishCheckCommands("redfish_command", category, commands, redfishCommandCategories); !ok {
		return res, nil
	}
	baseuri, username, password, res, ok := redfishtoolCredentials("redfish_command", args)
	if !ok {
		return res, nil
	}
	if res, ok := redfishtoolRequireBinary(ctx, conn, "redfish_command"); !ok {
		return res, nil
	}

	changed := false
	session := map[string]any{}
	returnValues := map[string]any{}
	for _, command := range commands {
		switch {
		case category == "Systems" && strings.HasPrefix(command, "Power"):
			r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "Systems", "reset", redfishResetTypeByCommand[command])
			if err != nil {
				return Result{}, err
			}
			if r.RC != 0 {
				return Fail("redfish_command: " + command + ": " + redfishtoolErrMsg(r)), nil
			}
			changed = true

		case category == "Systems" && (command == "SetOneTimeBoot" || command == "EnableContinuousBootOverride" || command == "DisableBootOverride"):
			res, err := redfishSetBootOverride(ctx, conn, baseuri, username, password, command, args)
			if err != nil {
				return Result{}, err
			}
			if res.Failed {
				return res, nil
			}
			changed = true

		case (category == "Systems" || category == "Chassis") && strings.HasPrefix(command, "IndicatorLed"):
			r, err := redfishtoolRun(ctx, conn, baseuri, username, password, category, "setIndicatorLed", redfishLedPayloadByCommand[command])
			if err != nil {
				return Result{}, err
			}
			if r.RC != 0 {
				return Fail("redfish_command: " + command + ": " + redfishtoolErrMsg(r)), nil
			}
			changed = true

		case category == "Sessions":
			res, sessionOut, err := redfishSessionCommand(ctx, conn, baseuri, username, password, command, args)
			if err != nil {
				return Result{}, err
			}
			if res.Failed {
				return res, nil
			}
			if sessionOut != nil {
				session = sessionOut
			}
			if res.Changed {
				changed = true
			}

		case category == "Accounts":
			res, err := redfishAccountCommand(ctx, conn, baseuri, username, password, command, args)
			if err != nil {
				return Result{}, err
			}
			if res.Failed {
				return res, nil
			}
			if res.Changed {
				changed = true
			}

		case category == "Manager":
			res, err := redfishManagerCommand(ctx, conn, baseuri, username, password, command, args)
			if err != nil {
				return Result{}, err
			}
			if res.Failed {
				return res, nil
			}
			if res.Changed {
				changed = true
			}
		}
	}

	out := Ok("Action was successful")
	if changed {
		out = Changed("Action was successful")
	}
	return out.WithExtra("session", session).WithExtra("return_values", returnValues), nil
}

var redfishCommandCategories = map[string][]string{
	"Systems": {
		"PowerOn", "PowerForceOff", "PowerForceRestart", "PowerGracefulRestart",
		"PowerGracefulShutdown", "PowerReboot", "PowerCycle",
		"SetOneTimeBoot", "EnableContinuousBootOverride", "DisableBootOverride",
		"IndicatorLedOn", "IndicatorLedOff", "IndicatorLedBlink",
	},
	"Chassis": {"IndicatorLedOn", "IndicatorLedOff", "IndicatorLedBlink"},
	"Accounts": {
		"AddUser", "DeleteUser", "EnableUser", "DisableUser",
		"UpdateUserRole", "UpdateUserPassword", "UpdateAccountServiceProperties",
	},
	"Sessions": {"ClearSessions", "CreateSession", "DeleteSession"},
	"Manager": {
		"PowerOn", "PowerForceOff", "PowerForceRestart", "PowerGracefulRestart",
		"PowerGracefulShutdown", "PowerReboot", "GracefulRestart", "ClearLogs",
	},
	"Update": {},
}

var redfishResetTypeByCommand = map[string]string{
	"PowerOn":               "On",
	"PowerForceOff":         "ForceOff",
	"PowerForceRestart":     "ForceRestart",
	"PowerGracefulRestart":  "GracefulRestart",
	"PowerGracefulShutdown": "GracefulShutdown",
	"PowerReboot":           "GracefulRestart",
	"PowerCycle":            "PowerCycle",
}

var redfishLedPayloadByCommand = map[string]string{
	"IndicatorLedOn":    "Lit",
	"IndicatorLedOff":   "Off",
	"IndicatorLedBlink": "Blinking",
}

// redfishSetBootOverride maps real redfish_command.py's `bootdevice`/
// `uefi_target`/`boot_next`/`boot_override_mode` args onto `redfishtool
// Systems setBootOverride <enabledVal> [<targetVal>]` (confirmed from
// Systems.py's own setBootOverride_single source before writing this).
//
// Two real, disclosed gaps fail loud rather than silently diverge:
//
//   - bootdevice in (UefiTarget, UefiBootNext) — real Ansible sets an
//     ADDITIONAL payload field (UefiTargetBootSourceOverride/BootNext)
//     for these two specific targets; redfishtool's own setBootOverride
//     syntax has no argument for it at all.
//   - boot_override_mode, when explicitly given — real Ansible adds
//     BootSourceOverrideMode to the patch; redfishtool's own
//     setBootOverride only ever passes back the CURRENT resource's own
//     mode unchanged, never a caller-requested one.
//
// Any other bootdevice value is passed through as-is to redfishtool,
// which validates it itself against the live resource's own
// AllowableValues — this port does not maintain a second, possibly
// stale copy of that enum.
func redfishSetBootOverride(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, command string, args map[string]any) (Result, error) {
	if bootOverrideMode := argString(args, "boot_override_mode", ""); bootOverrideMode != "" {
		return Fail("redfish_command: boot_override_mode is not settable by this port's redfishtool substitution — see redfish_command.go's own doc comment"), nil
	}

	enabledVal := map[string]string{
		"SetOneTimeBoot":               "Once",
		"EnableContinuousBootOverride": "Continuous",
		"DisableBootOverride":          "Disabled",
	}[command]

	if enabledVal == "Disabled" {
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "Systems", "setBootOverride", "Disabled")
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: " + command + ": " + redfishtoolErrMsg(r)), nil
		}
		return Changed(""), nil
	}

	bootdevice := argString(args, "bootdevice", "")
	if bootdevice == "" {
		return Fail("redfish_command: " + command + ": bootdevice option required for temporary boot override"), nil
	}
	if bootdevice == "UefiTarget" || bootdevice == "UefiBootNext" {
		return Fail("redfish_command: bootdevice " + bootdevice + " is not supported by this port's redfishtool substitution — see redfish_command.go's own doc comment"), nil
	}

	r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "Systems", "setBootOverride", enabledVal, bootdevice)
	if err != nil {
		return Result{}, err
	}
	if r.RC != 0 {
		return Fail("redfish_command: " + command + ": " + redfishtoolErrMsg(r)), nil
	}
	return Changed(""), nil
}

// redfishSessionCommand implements real redfish_command.py's Sessions
// category (ClearSessions/CreateSession/DeleteSession) via redfishtool's
// `SessionService` subcommand — its `login`/`logout` operations
// confirmed from SessionService.py's own source before writing this.
//
// CreateSession's real return shape is `session: {token, uri}` (real
// Ansible's own create_session reads the X-Auth-Token/Location response
// headers directly; redfishtool's own `login` operation already surfaces
// the equivalent SessionId/SessionLocation/X-Auth-Token as its JSON
// output instead, read via redfishtoolRunJSON) — this function's second
// return value carries that map, nil for the other two commands,
// matching real Ansible's own session=dict() default.
//
// ClearSessions has no single redfishtool bulk operation: this port
// lists Sessions (`SessionService Sessions list`), then logs each one
// out individually (`-l<uri> SessionService logout`) — an empty list is
// changed=false, matching real clear_sessions's own idempotent no-op.
func redfishSessionCommand(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, command string, args map[string]any) (Result, map[string]any, error) {
	switch command {
	case "ClearSessions":
		var coll struct {
			Members []struct {
				ODataID string `json:"@odata.id"`
			} `json:"Members"`
		}
		r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &coll, "SessionService", "Sessions", "list")
		if err != nil {
			return Result{}, nil, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: ClearSessions: " + redfishtoolErrMsg(r)), nil, nil
		}
		if len(coll.Members) == 0 {
			return Ok("There are no active sessions"), nil, nil
		}
		for _, m := range coll.Members {
			lr, err := redfishtoolRun(ctx, conn, baseuri, username, password, "-l", m.ODataID, "SessionService", "logout")
			if err != nil {
				return Result{}, nil, err
			}
			if lr.RC != 0 {
				return Fail("redfish_command: ClearSessions: " + redfishtoolErrMsg(lr)), nil, nil
			}
		}
		return Changed("Cleared all sessions successfully"), nil, nil

	case "CreateSession":
		if username == "" || password == "" {
			return Fail("redfish_command: CreateSession: must provide the username and password parameters"), nil, nil
		}
		var out struct {
			SessionID       string `json:"SessionId"`
			SessionLocation string `json:"SessionLocation"`
			AuthToken       string `json:"X-Auth-Token"`
		}
		r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &out, "SessionService", "login")
		if err != nil {
			return Result{}, nil, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: CreateSession: " + redfishtoolErrMsg(r)), nil, nil
		}
		session := map[string]any{"token": out.AuthToken, "uri": out.SessionLocation}
		return Changed("Session created successfully"), session, nil

	case "DeleteSession":
		sessionURI := argString(args, "session_uri", "")
		if sessionURI == "" {
			return Fail("redfish_command: DeleteSession: must provide the session_uri parameter"), nil, nil
		}
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "-l", sessionURI, "SessionService", "logout")
		if err != nil {
			return Result{}, nil, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: DeleteSession: " + redfishtoolErrMsg(r)), nil, nil
		}
		return Changed("Session deleted successfully"), nil, nil
	}
	return Fail("redfish_command: Sessions: unsupported command " + command), nil, nil
}

// redfishAccountCommand implements real redfish_command.py's Accounts
// category via redfishtool's `AccountService` subcommand — every
// command's exact syntax confirmed from AccountService.py's own source
// (addUser/deleteUser/userAdmin/setPassword/patch) before writing this.
//
// # Real idempotency, matched via redfishtool's own confirmed error text
//
// Real add_user/delete_user pre-check whether the account already
// exists (or doesn't) and return changed=false rather than erroring.
// redfishtool's own `adduser`/`deleteuser` instead error out in exactly
// those cases, with fixed, confirmed message text ("username %s already
// exists" / "username %s does not exists" — the latter's own grammar,
// not a typo introduced here) — this port matches on that exact text to
// recover the same idempotent no-op, rather than adding a second,
// separate existence-check call.
//
// # Two disclosed gaps
//
// UpdateUserName is not wired: redfishtool's own `setusername <id>
// <name>` needs the account's Redfish Id, not its current username, and
// resolving one from the other needs a second listing call this
// increment doesn't add. UpdateUserAccountTypes is not wired: redfishtool
// has no AccountTypes/OEMAccountTypes operation at all. Both are absent
// from redfishCommandCategories' own Accounts list, so both fail loud
// through the standard redfishCheckCommands path.
func redfishAccountCommand(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, command string, args map[string]any) (Result, error) {
	accountUsername := argString(args, "account_username", "")

	switch command {
	case "AddUser":
		if accountUsername == "" {
			return Fail("redfish_command: AddUser: must provide account_username"), nil
		}
		if argString(args, "account_accounttypes", "") != "" || argString(args, "account_oemaccounttypes", "") != "" || argString(args, "account_id", "") != "" {
			return Fail("redfish_command: AddUser: account_accounttypes/account_oemaccounttypes/account_id are not settable by this port's redfishtool substitution — see redfish_command.go's own doc comment"), nil
		}
		accountPassword := argString(args, "account_password", "")
		roleArgs := []string{"AccountService", "adduser", accountUsername, accountPassword}
		if roleID := argString(args, "account_roleid", ""); roleID != "" {
			roleArgs = append(roleArgs, roleID)
		}
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, roleArgs...)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			if strings.Contains(redfishtoolErrMsg(r), "already exists") {
				return Ok(""), nil
			}
			return Fail("redfish_command: AddUser: " + redfishtoolErrMsg(r)), nil
		}
		return Changed(""), nil

	case "DeleteUser":
		if accountUsername == "" {
			return Fail("redfish_command: DeleteUser: must provide account_username"), nil
		}
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "AccountService", "deleteuser", accountUsername)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			if strings.Contains(redfishtoolErrMsg(r), "does not exist") {
				return Ok(""), nil
			}
			return Fail("redfish_command: DeleteUser: " + redfishtoolErrMsg(r)), nil
		}
		return Changed(""), nil

	case "EnableUser", "DisableUser":
		if accountUsername == "" {
			return Fail("redfish_command: " + command + ": must provide account_username"), nil
		}
		action := map[string]string{"EnableUser": "enable", "DisableUser": "disable"}[command]
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "AccountService", "useradmin", accountUsername, action)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: " + command + ": " + redfishtoolErrMsg(r)), nil
		}
		return Changed(""), nil

	case "UpdateUserRole":
		if accountUsername == "" {
			return Fail("redfish_command: UpdateUserRole: must provide account_username"), nil
		}
		roleID := argString(args, "account_roleid", "")
		if roleID == "" {
			return Fail("redfish_command: UpdateUserRole: must provide account_roleid"), nil
		}
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "AccountService", "useradmin", accountUsername, "setRoleId", roleID)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: UpdateUserRole: " + redfishtoolErrMsg(r)), nil
		}
		return Changed(""), nil

	case "UpdateUserPassword":
		if accountUsername == "" {
			return Fail("redfish_command: UpdateUserPassword: must provide account_username"), nil
		}
		accountPassword := argString(args, "account_password", "")
		if accountPassword == "" {
			return Fail("redfish_command: UpdateUserPassword: must provide account_password"), nil
		}
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "AccountService", "setpassword", accountUsername, accountPassword)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: UpdateUserPassword: " + redfishtoolErrMsg(r)), nil
		}
		return Changed(""), nil

	case "UpdateAccountServiceProperties":
		accountProperties := args["account_properties"]
		props, ok := accountProperties.(map[string]any)
		if !ok || len(props) == 0 {
			return Fail("redfish_command: UpdateAccountServiceProperties: must provide account_properties"), nil
		}
		b, err := json.Marshal(props)
		if err != nil {
			return Result{}, err
		}
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "AccountService", "patch", string(b))
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: UpdateAccountServiceProperties: " + redfishtoolErrMsg(r)), nil
		}
		return Changed("Modified account service"), nil
	}
	return Fail("redfish_command: Accounts: unsupported command " + command), nil
}

// redfishManagerCommand implements real redfish_command.py's Manager
// category's Power*/GracefulRestart and ClearLogs commands via
// redfishtool's `Managers` subcommand — confirmed from Managers.py's
// own source (its `reset` operation shares the exact same resetType
// enum Systems.py's own `reset` does; its `Logs`/`clearLog <id>`
// operations, not a single bulk clear).
//
// # Real semantics matched
//
// `GracefulRestart` is a legacy alias real redfish_command.py itself
// rewrites to `PowerGracefulRestart` before dispatch — this port does
// the same rewrite. ClearLogs lists every LogServices member
// (`Managers Logs list`) and clears each individually (`Managers
// clearLog <id>`), matching real clear_logs()'s own loop-over-all-
// members behavior exactly, including reporting Changed unconditionally
// (real clear_logs never sets changed=false, even when no logs exist —
// confirmed from its own source, not assumed).
//
// # One real, disclosed gap
//
// real manage_manager_power accepts `wait`/`wait_timeout` to poll the
// manager until it comes back up before returning; redfishtool's own
// `reset` operation has no such option, so a task requesting `wait:
// true` fails loud rather than silently returning before the manager
// has actually finished restarting.
func redfishManagerCommand(ctx context.Context, conn remoteexec.Connection, baseuri, username, password, command string, args map[string]any) (Result, error) {
	if argBool(args, "wait", false) {
		return Fail("redfish_command: Manager: wait/wait_timeout is not supported by this port's redfishtool substitution — see redfish_command.go's own doc comment"), nil
	}

	switch {
	case command == "GracefulRestart" || strings.HasPrefix(command, "Power"):
		resetType := "GracefulRestart"
		if command != "GracefulRestart" {
			resetType = redfishResetTypeByCommand[command]
		}
		r, err := redfishtoolRun(ctx, conn, baseuri, username, password, "Managers", "reset", resetType)
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: " + command + ": " + redfishtoolErrMsg(r)), nil
		}
		return Changed(""), nil

	case command == "ClearLogs":
		var coll struct {
			Members []struct {
				ID string `json:"Id"`
			} `json:"Members"`
		}
		r, err := redfishtoolRunJSON(ctx, conn, baseuri, username, password, &coll, "Managers", "Logs", "list")
		if err != nil {
			return Result{}, err
		}
		if r.RC != 0 {
			return Fail("redfish_command: ClearLogs: " + redfishtoolErrMsg(r)), nil
		}
		for _, m := range coll.Members {
			lr, err := redfishtoolRun(ctx, conn, baseuri, username, password, "Managers", "clearLog", m.ID)
			if err != nil {
				return Result{}, err
			}
			if lr.RC != 0 {
				return Fail("redfish_command: ClearLogs: " + redfishtoolErrMsg(lr)), nil
			}
		}
		return Changed(""), nil
	}
	return Fail("redfish_command: Manager: unsupported command " + command), nil
}
