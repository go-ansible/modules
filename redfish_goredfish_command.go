package modules

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-redfish/redfish"
)

// redfishCommandGoCategories is real redfish_command's CATEGORY_COMMANDS_ALL
// (community.general at the commit go-redfish's PARITY.md names): the
// default backend implements a subset of it, the go-redfish backend all of
// it.
var redfishCommandGoCategories = map[string][]string{
	"Systems": {
		"PowerOn", "PowerForceOff", "PowerForceRestart", "PowerGracefulRestart",
		"PowerGracefulShutdown", "PowerReboot", "PowerCycle", "PowerFullPowerCycle",
		"SetOneTimeBoot", "EnableContinuousBootOverride", "DisableBootOverride",
		"IndicatorLedOn", "IndicatorLedOff", "IndicatorLedBlink",
		"VirtualMediaInsert", "VirtualMediaEject", "VerifyBiosAttributes",
	},
	"Chassis": {"IndicatorLedOn", "IndicatorLedOff", "IndicatorLedBlink"},
	"Accounts": {
		"AddUser", "EnableUser", "DeleteUser", "DisableUser", "UpdateUserRole",
		"UpdateUserPassword", "UpdateUserName", "UpdateUserAccountTypes", "UpdateAccountServiceProperties",
	},
	"Sessions": {"ClearSessions", "CreateSession", "DeleteSession"},
	"Manager": {
		"GracefulRestart", "ClearLogs", "VirtualMediaInsert", "ResetToDefaults", "VirtualMediaEject",
		"PowerOn", "PowerForceOff", "PowerForceRestart", "PowerGracefulRestart", "PowerGracefulShutdown", "PowerReboot",
	},
	"Update": {"SimpleUpdate", "MultipartHTTPPushUpdate", "PerformRequestedOperations"},
}

// cmdOutcome is what one command leaves: real keeps only the LAST command's
// outcome of a task (result is overwritten in its loop), and so does this.
type cmdOutcome struct {
	changed bool
	session map[string]any
	err     error
}

func redfishCommandGo(ctx context.Context, args map[string]any, category string, commands []string) (Result, error) {
	if res, ok := redfishGoValidate("redfish_command", []string{"Systems", "Chassis", "Accounts", "Sessions", "Manager", "Update"},
		redfishCommandGoCategories, category, commands); !ok {
		return res, nil
	}
	c, res, ok := redfishGoClient("redfish_command", args, 60)
	if !ok {
		return res, nil
	}
	returnValues := map[string]any{}
	out := cmdOutcome{changed: true}
	for _, command := range commands {
		out = redfishCommandGoOne(ctx, c, args, category, command, returnValues)
	}
	if out.err != nil {
		return Fail(redfishGoCleanMsg(out.err)), nil
	}
	session := out.session
	if session == nil {
		session = map[string]any{}
	}
	r := Ok("Action was successful")
	r.Changed = out.changed
	return r.WithExtra("session", session).WithExtra("return_values", returnValues), nil
}

func fromOutcome(o redfish.Outcome, err error) cmdOutcome {
	if err != nil {
		return cmdOutcome{err: err}
	}
	return cmdOutcome{changed: o.Changed}
}

func argMapOf(args map[string]any, key string) map[string]any {
	m, _ := args[key].(map[string]any)
	return m
}

func boolPtr(m map[string]any, key string, def bool) *bool {
	b := argBool(m, key, def)
	return &b
}

func redfishAccountSpec(args map[string]any) redfish.AccountSpec {
	return redfish.AccountSpec{
		ID:              argStringAliased(args, "id", "account_id", ""),
		Username:        argStringAliased(args, "new_username", "account_username", ""),
		Password:        redfish.Secret(argStringAliased(args, "new_password", "account_password", "")),
		RoleID:          argStringAliased(args, "roleid", "account_roleid", ""),
		AccountTypes:    argStringListAliased(args, "account_types", "account_accounttypes"),
		OEMAccountTypes: argStringListAliased(args, "oem_account_types", "account_oemaccounttypes"),
		NewUsername:     argStringAliased(args, "update_username", "account_updatename", ""),
	}
}

func redfishCommandGoOne(ctx context.Context, c *redfish.Client, args map[string]any, category, command string, rv map[string]any) cmdOutcome {
	resource := argString(args, "resource_id", "")
	switch category {
	case "Accounts":
		a := redfishAccountSpec(args)
		switch command {
		case "AddUser":
			return fromOutcome(c.AddUser(ctx, a))
		case "EnableUser":
			return fromOutcome(c.EnableUser(ctx, a))
		case "DeleteUser":
			return fromOutcome(c.DeleteUser(ctx, a))
		case "DisableUser":
			return fromOutcome(c.DisableUser(ctx, a))
		case "UpdateUserRole":
			return fromOutcome(c.UpdateUserRole(ctx, a))
		case "UpdateUserPassword":
			return fromOutcome(c.UpdateUserPassword(ctx, a))
		case "UpdateUserName":
			return fromOutcome(c.UpdateUserName(ctx, a))
		case "UpdateUserAccountTypes":
			return fromOutcome(c.UpdateUserAccountTypes(ctx, a))
		case "UpdateAccountServiceProperties":
			return fromOutcome(c.UpdateAccountServiceProperties(ctx, argMapOf(args, "account_properties")))
		}

	case "Systems":
		u, err := c.ResolveSystem(ctx, resource)
		if err != nil {
			return cmdOutcome{err: err}
		}
		switch {
		case strings.HasPrefix(command, "Power"):
			return fromOutcome(c.SystemPower(ctx, u, command))
		case command == "SetOneTimeBoot", command == "EnableContinuousBootOverride", command == "DisableBootOverride":
			enabled := map[string]string{"SetOneTimeBoot": "Once", "EnableContinuousBootOverride": "Continuous", "DisableBootOverride": "Disabled"}[command]
			return fromOutcome(c.SetBootOverride(ctx, u, redfish.BootOverride{
				Device: argString(args, "bootdevice", ""), UefiTarget: argString(args, "uefi_target", ""),
				BootNext: argString(args, "boot_next", ""), Enabled: enabled, Mode: argString(args, "boot_override_mode", ""),
			}))
		case strings.HasPrefix(command, "IndicatorLed"):
			return fromOutcome(c.SetIndicatorLED(ctx, u, command))
		case command == "VirtualMediaInsert":
			return fromOutcome(c.VirtualMediaInsert(ctx, u, redfishVirtualMedia(args)))
		case command == "VirtualMediaEject":
			return fromOutcome(c.VirtualMediaEject(ctx, u, argString(argMapOf(args, "virtual_media"), "image_url", "")))
		case command == "VerifyBiosAttributes":
			return fromOutcome(c.VerifyBiosAttributes(ctx, u, argMapOf(args, "bios_attributes")))
		}

	case "Chassis":
		u, err := c.ResolveChassis(ctx, resource)
		if err != nil {
			return cmdOutcome{err: err}
		}
		return fromOutcome(c.SetIndicatorLED(ctx, u, command))

	case "Sessions":
		switch command {
		case "ClearSessions":
			return fromOutcome(c.ClearSessions(ctx))
		case "CreateSession":
			if argString(args, "username", "") == "" || argString(args, "password", "") == "" {
				return cmdOutcome{err: errors.New("Must provide the username and password parameters for the CreateSession command")}
			}
			if err := c.Login(ctx); err != nil {
				return cmdOutcome{err: err}
			}
			tok, uri := c.Session()
			return cmdOutcome{changed: true, session: map[string]any{"token": tok, "uri": uri}}
		case "DeleteSession":
			_, err := c.Delete(ctx, argString(args, "session_uri", ""), "")
			return cmdOutcome{changed: err == nil, err: err}
		}

	case "Manager":
		u, err := c.ResolveManager(ctx, resource)
		if err != nil {
			return cmdOutcome{err: err}
		}
		switch {
		case command == "GracefulRestart", strings.HasPrefix(command, "Power"):
			o, err := c.ManagerPower(ctx, u, command)
			if err == nil && argBool(args, "wait", false) {
				timeout := time.Duration(argInt(args, "wait_timeout", 120)) * time.Second
				err = c.WaitAvailable(ctx, timeout/24, timeout/24, timeout)
			}
			return fromOutcome(o, err)
		case command == "ClearLogs":
			return fromOutcome(c.ClearLogs(ctx, u))
		case command == "VirtualMediaInsert":
			return fromOutcome(c.VirtualMediaInsert(ctx, u, redfishVirtualMedia(args)))
		case command == "VirtualMediaEject":
			return fromOutcome(c.VirtualMediaEject(ctx, u, argString(argMapOf(args, "virtual_media"), "image_url", "")))
		case command == "ResetToDefaults":
			return fromOutcome(c.ManagerResetToDefaults(ctx, u, argString(args, "reset_to_defaults_mode", "")))
		}

	case "Update":
		switch command {
		case "SimpleUpdate":
			creds := argMapOf(args, "update_creds")
			r, err := c.SimpleUpdate(ctx, redfish.SimpleUpdateOptions{
				ImageURI: argString(args, "update_image_uri", ""), Protocol: argString(args, "update_protocol", ""),
				Targets: argStringList(args, "update_targets"), Username: argString(creds, "username", ""),
				Password: redfish.Secret(argString(creds, "password", "")), ApplyTime: argString(args, "update_apply_time", ""),
			})
			if err != nil {
				return cmdOutcome{err: err}
			}
			rv["update_status"] = statusMap(r.Status)
			return cmdOutcome{changed: r.Changed}
		case "MultipartHTTPPushUpdate":
			custom := ""
			switch v := args["update_custom_oem_params"].(type) {
			case string:
				custom = v
			case nil:
			default:
				b, _ := json.Marshal(v)
				custom = string(b)
			}
			r, err := c.MultipartHTTPPushUpdate(ctx, redfish.PushOptions{
				ImageFile: argString(args, "update_image_file", ""), Targets: argStringList(args, "update_targets"),
				ApplyTime: argString(args, "update_apply_time", ""), OEMParams: argMapOf(args, "update_oem_params"),
				CustomHeader: argString(args, "update_custom_oem_header", ""), CustomMIMEType: argString(args, "update_custom_oem_mime_type", ""),
				CustomParams: custom,
			})
			if err != nil {
				return cmdOutcome{err: err}
			}
			rv["update_status"] = statusMap(r.Status)
			return cmdOutcome{changed: r.Changed}
		case "PerformRequestedOperations":
			return fromOutcome(c.PerformRequestedOperations(ctx, argString(args, "update_handle", "")))
		}
	}
	return cmdOutcome{err: errors.New("unsupported command " + category + "/" + command)}
}

func redfishVirtualMedia(args map[string]any) redfish.VirtualMediaOptions {
	vm := argMapOf(args, "virtual_media")
	return redfish.VirtualMediaOptions{
		ImageURL: argString(vm, "image_url", ""), MediaTypes: argStringList(vm, "media_types"),
		Inserted: boolPtr(vm, "inserted", true), WriteProtected: boolPtr(vm, "write_protected", true),
		Username: argString(vm, "username", ""), Password: argString(vm, "password", ""),
		TransferProtocolType: argString(vm, "transfer_protocol_type", ""), TransferMethod: argString(vm, "transfer_method", ""),
	}
}

func statusMap(s redfish.UpdateStatus) map[string]any {
	b, _ := json.Marshal(s)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["ret"] = true // real's update_status dict carries it
	return m
}
