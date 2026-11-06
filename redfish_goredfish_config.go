package modules

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-redfish/redfish"
)

// redfishConfigGoCategories is real redfish_config's CATEGORY_COMMANDS_ALL.
var redfishConfigGoCategories = map[string][]string{
	"Systems": {
		"SetBiosDefaultSettings", "SetBiosAttributes", "SetBootOrder", "SetDefaultBootOrder",
		"EnableSecureBoot", "SetSecureBoot", "DeleteVolumes", "CreateVolume", "SetPowerRestorePolicy",
	},
	"Manager":  {"SetNetworkProtocols", "SetManagerNic", "SetHostInterface", "SetServiceIdentification"},
	"Sessions": {"SetSessionService"},
}

// redfishConfigGo is redfish_config on the go-redfish backend. As in real,
// only the last command's outcome decides the result.
func redfishConfigGo(ctx context.Context, args map[string]any, category string, commands []string) (Result, error) {
	if res, ok := redfishGoValidate("redfish_config", []string{"Systems", "Manager", "Sessions"},
		redfishConfigGoCategories, category, commands); !ok {
		return res, nil
	}
	c, res, ok := redfishGoClient("redfish_config", args, 60)
	if !ok {
		return res, nil
	}
	var out redfish.Outcome
	var err error
	for _, command := range commands {
		out, err = redfishConfigGoOne(ctx, c, args, category, command)
	}
	if err != nil {
		return Fail(redfishGoCleanMsg(err)), nil
	}
	msg := redfishGoMsg(args, out.Message)
	if category == "Systems" && commands[len(commands)-1] == "DeleteVolumes" && out.Changed {
		msg = "The following volumes were deleted: " + pyRepr(argStringList(args, "volume_ids"))
	}
	r := Ok(msg)
	r.Changed = out.Changed
	if out.Warning != "" {
		r = r.WithExtra("warnings", []any{out.Warning})
	}
	return r, nil
}

func redfishConfigGoOne(ctx context.Context, c *redfish.Client, args map[string]any, category, command string) (redfish.Outcome, error) {
	resource := argString(args, "resource_id", "")
	switch category {
	case "Systems":
		u, err := c.ResolveSystem(ctx, resource)
		if err != nil {
			return redfish.Outcome{}, err
		}
		switch command {
		case "SetBiosDefaultSettings":
			return c.ResetBios(ctx, u)
		case "SetBiosAttributes":
			return c.SetBiosAttributes(ctx, u, argMapOf(args, "bios_attributes"), "")
		case "SetBootOrder":
			return c.SetBootOrder(ctx, u, argStringList(args, "boot_order"))
		case "SetDefaultBootOrder":
			return c.SetDefaultBootOrder(ctx, u)
		case "EnableSecureBoot":
			return c.SetSecureBoot(ctx, u, true)
		case "SetSecureBoot":
			return c.SetSecureBoot(ctx, u, argBool(args, "secure_boot_enable", true))
		case "SetPowerRestorePolicy":
			return c.SetPowerRestorePolicy(ctx, u, argString(args, "power_restore_policy", ""))
		case "DeleteVolumes":
			r, err := c.DeleteVolumes(ctx, u, argString(args, "storage_subsystem_id", ""), argStringList(args, "volume_ids"), redfish.DeleteVolumesOptions{})
			return r.Outcome, err
		case "CreateVolume":
			details := argMapOf(args, "volume_details")
			drives := []string{}
			if l, ok := details["Drives"].([]any); ok {
				for _, d := range l {
					drives = append(drives, fmt.Sprint(d))
				}
			}
			rest := map[string]any{}
			for k, v := range details {
				if k != "Drives" {
					rest[k] = v
				}
			}
			r, err := c.CreateVolume(ctx, u, argString(args, "storage_subsystem_id", ""), redfish.VolumeRequest{
				Details: rest, Drives: drives, DeleteNoneVolumes: argBool(args, "storage_none_volume_deletion", false),
			})
			return r.Outcome, err
		}
	case "Manager":
		u, err := c.ResolveManager(ctx, resource)
		if err != nil {
			return redfish.Outcome{}, err
		}
		switch command {
		case "SetNetworkProtocols":
			return c.SetNetworkProtocols(ctx, u, argMapOf(args, "network_protocols"))
		case "SetManagerNic":
			return c.SetManagerNIC(ctx, u, argString(args, "nic_addr", "null"), argMapOf(args, "nic_config"))
		case "SetHostInterface":
			return c.SetHostInterface(ctx, u, argString(args, "hostinterface_id", ""), argMapOf(args, "hostinterface_config"))
		case "SetServiceIdentification":
			return c.SetServiceIdentification(ctx, u, argString(args, "service_id", ""))
		}
	case "Sessions":
		return c.SetSessionService(ctx, argMapOf(args, "sessions_config"))
	}
	return redfish.Outcome{}, errors.New("unsupported command " + category + "/" + command)
}
