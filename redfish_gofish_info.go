package modules

import (
	"context"
	"fmt"

	"github.com/go-redfish/redfish"
)

// redfishInfoGo is redfish_info on the go-redfish backend: every command of
// redfishInfoCategories is mapped to the client function PARITY.md of
// go-redfish names for it, and its answer is put in the shape real
// community.general's redfish_info returns (redfish_facts.<key>).
func redfishInfoGo(ctx context.Context, args map[string]any, categories, requested []string) (Result, error) {
	c, res, ok := redfishGoClient("redfish_info", args, 60)
	if !ok {
		return res, nil
	}
	facts := map[string]any{}
	for _, category := range categories {
		if res, ok := redfishCheckCategory("redfish_info", category, redfishInfoCategories); !ok {
			return res, nil
		}
		commands := requested
		if len(commands) == 0 {
			commands = []string{redfishInfoDefaultCommand[category]}
		}
		if res, ok := redfishCheckCommands("redfish_info", category, commands, redfishInfoCategories); !ok {
			return res, nil
		}
		for _, command := range commands {
			key, v, err := redfishInfoGoOne(ctx, c, args, category, command)
			if err != nil {
				return redfishGoFail("redfish_info", err), nil
			}
			if key == "" {
				return redfishGoUnsupported("redfish_info", category, command), nil
			}
			facts[key] = v
		}
	}
	return Ok("").WithExtra("redfish_facts", facts), nil
}

// perSystem runs fn on every system and aggregates the way real
// aggregate_systems does: entries is a list of [{system_uri: uri}, value].
func perSystem(ctx context.Context, c *redfish.Client, fn func(uri string) (any, error)) (any, error) {
	uris, err := c.SystemURIs(ctx)
	if err != nil {
		return nil, err
	}
	entries := []any{}
	for _, u := range uris {
		v, err := fn(u)
		if err != nil {
			return nil, err
		}
		entries = append(entries, []any{map[string]any{"system_uri": u}, v})
	}
	return map[string]any{"entries": entries}, nil
}

func perManager(ctx context.Context, c *redfish.Client, fn func(uri string) (any, error)) (any, error) {
	uris, err := c.ManagerURIs(ctx)
	if err != nil {
		return nil, err
	}
	entries := []any{}
	for _, u := range uris {
		v, err := fn(u)
		if err != nil {
			return nil, err
		}
		entries = append(entries, []any{map[string]any{"manager_uri": u}, v})
	}
	return map[string]any{"entries": entries}, nil
}

func entries(v any) any { return map[string]any{"entries": v} }

func redfishInfoGoOne(ctx context.Context, c *redfish.Client, args map[string]any, category, command string) (string, any, error) {
	switch category + "/" + command {
	case "Service/CheckAvailability":
		if _, err := c.Get(ctx, "/redfish"); err != nil {
			return "", nil, err
		}
		return "service", map[string]any{"msg": "Service is available."}, nil

	case "Systems/GetSystemInventory":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.SystemInventory(ctx, u) })
		return "system", v, err
	case "Systems/GetBootOverride":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.BootOverrideOf(ctx, u) })
		return "boot_override", v, err
	case "Systems/GetPowerRestorePolicy":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.PowerRestorePolicy(ctx, u) })
		return "power_restore_policy", v, err
	case "Systems/GetHealthReport":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.SystemHealthReport(ctx, u) })
		return "health_report", v, err
	case "Systems/GetNicInventory":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.NICInventory(ctx, u) })
		return "nic", v, err
	case "Systems/GetVirtualMedia":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.VirtualMedia(ctx, u) })
		return "virtual_media", v, err
	case "Systems/GetCpuInventory":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.CPUInventory(ctx, u) })
		return "cpu", v, err
	case "Systems/GetMemoryInventory":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.MemoryInventory(ctx, u) })
		return "memory", v, err
	case "Systems/GetBiosAttributes":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.BiosAttributes(ctx, u) })
		return "bios_attribute", v, err
	case "Systems/GetBootOrder":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.BootOrder(ctx, u) })
		return "boot_order", v, err
	case "Systems/GetStorageControllerInventory":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.StorageControllerInventory(ctx, u) })
		return "storage_controller", v, err
	case "Systems/GetDiskInventory":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.DiskInventory(ctx, u) })
		return "disk", v, err
	case "Systems/GetVolumeInventory":
		v, err := perSystem(ctx, c, func(u string) (any, error) { return c.VolumeInventory(ctx, u) })
		return "volume", v, err

	case "Chassis/GetChassisInventory":
		v, err := c.ChassisInventory(ctx)
		return "chassis", entries(v), err
	case "Chassis/GetFanInventory":
		v, err := c.FanInventory(ctx)
		return "fan", entries(v), err
	case "Chassis/GetChassisPower":
		v, err := c.ChassisPower(ctx)
		return "chassis_power", entries(v), err
	case "Chassis/GetHealthReport":
		v, err := c.ChassisHealthReports(ctx)
		return "health_report", entries(v), err
	case "Chassis/GetChassisThermals":
		v, err := c.ChassisThermals(ctx)
		return "thermals", entries(v), err
	case "Chassis/GetPsuInventory":
		v, err := c.PSUInventory(ctx)
		return "psu", entries(v), err
	case "Chassis/GetHPEThermalConfig":
		v, err := c.HPEThermalConfig(ctx)
		return "hpe_thermal_config", entries(v), err
	case "Chassis/GetHPEFanPercentMin":
		v, err := c.HPEFanPercentMin(ctx)
		return "hpe_fan_percent_min", entries(v), err

	case "Accounts/ListUsers":
		v, err := c.ListUsers(ctx)
		return "user", entries(v), err
	case "Accounts/GetAccountServiceConfig":
		v, err := c.AccountServiceConfig(ctx)
		return "accountservice_config", entries(v), err
	case "Sessions/GetSessions":
		v, err := c.GetSessions(ctx)
		return "session", entries(v), err

	case "Update/GetFirmwareInventory":
		v, err := c.FirmwareInventory(ctx)
		return "firmware", entries(v), err
	case "Update/GetSoftwareInventory":
		v, err := c.SoftwareInventory(ctx)
		return "software", entries(v), err
	case "Update/GetFirmwareUpdateCapabilities":
		v, err := c.FirmwareUpdateCapabilities(ctx)
		return "firmware_update_capabilities", map[string]any{"multipart_supported": v.MultipartSupported, "entries": v.Entries}, err

	case "Manager/GetManagerInventory":
		v, err := perManager(ctx, c, func(u string) (any, error) { return c.ManagerInventory(ctx, u) })
		return "manager", v, err
	case "Manager/GetNetworkProtocols":
		v, err := c.NetworkProtocols(ctx, mustManager(ctx, c))
		return "network_protocols", entries(v), err
	case "Manager/GetServiceIdentification":
		v, err := c.ResolveServiceIdentification(ctx, argString(args, "manager", ""))
		return "service_id", map[string]any{"entries": v}, err
	case "Manager/GetManagerNicInventory":
		v, err := perManager(ctx, c, func(u string) (any, error) { return c.ManagerNICInventory(ctx, u) })
		return "manager_nics", v, err
	case "Manager/GetLogs":
		v, err := c.Logs(ctx, mustManager(ctx, c), 0)
		return "log", entries(v), err
	case "Manager/GetVirtualMedia":
		v, err := perManager(ctx, c, func(u string) (any, error) { return c.VirtualMedia(ctx, u) })
		return "virtual_media", v, err
	case "Manager/GetHostInterfaces":
		v, err := c.HostInterfaces(ctx)
		return "host_interfaces", entries(v), err
	case "Manager/GetHealthReport":
		v, err := perManager(ctx, c, func(u string) (any, error) { return c.ManagerHealthReport(ctx, u) })
		return "health_report", v, err
	}
	return "", nil, nil
}

// mustManager resolves the manager the command acts on; an error is carried
// as the empty URI, which the next call then reports.
func mustManager(ctx context.Context, c *redfish.Client) string {
	u, err := c.ResolveManager(ctx, "")
	if err != nil {
		return fmt.Sprintf("/redfish/v1/Managers/unresolved: %v", err)
	}
	return u
}
