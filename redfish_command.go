package modules

import (
	"context"
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
//
// Accounts, Sessions, Manager, Update, VirtualMediaInsert/Eject, and
// VerifyBiosAttributes are declared with empty command lists below —
// real categories, no commands wired yet — a later increment of this
// same batch, not assumed to work.
//
// Args: category (required); command (required list); baseuri
// (required, real effect); username/password (real effect, staged via
// a temp cfgFile, never argv); auth_token (not supported, fails loud —
// see redfishtool_common.go's own doc comment); bootdevice; uefi_target;
// boot_next; boot_override_mode.
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
		}
	}

	out := Ok("Action was successful")
	if changed {
		out = Changed("Action was successful")
	}
	return out.WithExtra("redfish_command", map[string]any{"ret": true}), nil
}

var redfishCommandCategories = map[string][]string{
	"Systems": {
		"PowerOn", "PowerForceOff", "PowerForceRestart", "PowerGracefulRestart",
		"PowerGracefulShutdown", "PowerReboot", "PowerCycle",
		"SetOneTimeBoot", "EnableContinuousBootOverride", "DisableBootOverride",
		"IndicatorLedOn", "IndicatorLedOff", "IndicatorLedBlink",
	},
	"Chassis":  {"IndicatorLedOn", "IndicatorLedOff", "IndicatorLedBlink"},
	"Accounts": {},
	"Sessions": {},
	"Manager":  {},
	"Update":   {},
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
