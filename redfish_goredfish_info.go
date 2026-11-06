package modules

import (
	"context"
	"errors"
	"strings"

	"github.com/go-redfish/redfish"
)

// redfishInfoGo is redfish_info on the go-redfish backend: every command of
// redfishInfoCategories is mapped to the client function PARITY.md of
// go-redfish names for it, and its answer is put in the shape real
// community.general's redfish_info returns (redfish_facts.<key>, each value
// a dict with ret, and entries or msg). The shapes were taken from real
// ansible running against the same mock BMC (redfish_interop_test.go), not
// from the module's documentation.
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
				// Real fails the task with the service's own message, without a prefix.
				return Fail(redfishGoCleanMsg(err)), nil
			}
			if key == "" {
				return redfishGoUnsupported("redfish_info", category, command), nil
			}
			facts[key] = v
		}
	}
	// Real redfish_info's success result carries no msg key.
	return Result{NoMsg: true}.WithExtra("redfish_facts", facts), nil
}

// redfishGoCleanMsg is the text of err without go-redfish's sentinel prefix
// ("redfish: resource not found: UpdateService resource not found" is
// "UpdateService resource not found", which is what real reports).
func redfishGoCleanMsg(err error) string {
	msg := err.Error()
	for _, s := range []error{redfish.ErrNotFound, redfish.ErrUnsupported, redfish.ErrAuth} {
		msg = strings.TrimPrefix(msg, s.Error()+": ")
	}
	return msg
}

func infoOK(entries any) map[string]any { return map[string]any{"ret": true, "entries": entries} }

// perResource runs fn on every resource URI and aggregates the way real does
// (aggregate_systems / aggregate_managers / get_multi_nic_inventory: entries
// is a list of [{<uriKey>: uri}, value]). A resource that has none of what
// fn reads (ErrUnsupported) is left out and makes ret false, as real's
// aggregate does.
func perResource(uris []string, uriKey string, fn func(uri string) (any, error)) (any, error) {
	entries := []any{}
	ret := true
	for _, u := range uris {
		v, err := fn(u)
		if errors.Is(err, redfish.ErrUnsupported) {
			ret = false
			continue
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, []any{map[string]any{uriKey: u}, v})
	}
	return map[string]any{"ret": ret, "entries": entries}, nil
}

func redfishInfoGoOne(ctx context.Context, c *redfish.Client, args map[string]any, category, command string) (string, any, error) {
	sys := func(key, uriKey string, fn func(u string) (any, error)) (string, any, error) {
		uris, err := c.SystemURIs(ctx)
		if err != nil {
			return key, nil, err
		}
		v, err := perResource(uris, uriKey, fn)
		return key, v, err
	}
	mgr := func(key, uriKey string, fn func(u string) (any, error)) (string, any, error) {
		uris, err := c.ManagerURIs(ctx)
		if err != nil {
			return key, nil, err
		}
		v, err := perResource(uris, uriKey, fn)
		return key, v, err
	}
	list := func(key string, v any, err error) (string, any, error) {
		if err != nil {
			return key, nil, err
		}
		return key, infoOK(v), nil
	}

	switch category + "/" + command {
	case "Service/CheckAvailability":
		r, err := c.Get(ctx, "/redfish/v1/")
		if err != nil {
			return "service", map[string]any{"ret": true, "available": false}, nil
		}
		var root map[string]any
		if err := r.JSON(&root); err != nil {
			return "service", map[string]any{"ret": true, "available": false}, nil
		}
		picked := map[string]any{}
		for _, k := range []string{"Id", "Name", "RedfishVersion", "Vendor", "ServiceIdentification", "ProtocolFeaturesSupported", "UUID"} {
			if v, ok := root[k]; ok {
				picked[k] = v
			}
		}
		return "service", map[string]any{"ret": true, "available": true, "entries": picked}, nil

	case "Systems/GetSystemInventory":
		return sys("system", "system_uri", func(u string) (any, error) { return c.SystemInventory(ctx, u) })
	case "Systems/GetBootOverride":
		return sys("boot_override", "system_uri", func(u string) (any, error) { return c.BootOverrideOf(ctx, u) })
	case "Systems/GetPowerRestorePolicy":
		return sys("power_restore_policy", "system_uri", func(u string) (any, error) { return c.PowerRestorePolicy(ctx, u) })
	case "Systems/GetHealthReport":
		return sys("health_report", "system_uri", func(u string) (any, error) { return c.SystemHealthReport(ctx, u) })
	case "Systems/GetNicInventory":
		return sys("nic", "resource_uri", func(u string) (any, error) { return c.NICInventory(ctx, u) })
	case "Systems/GetVirtualMedia":
		return sys("virtual_media", "resource_uri", func(u string) (any, error) { return c.VirtualMedia(ctx, u) })
	case "Systems/GetCpuInventory":
		return sys("cpu", "system_uri", func(u string) (any, error) { return c.CPUInventory(ctx, u) })
	case "Systems/GetMemoryInventory":
		return sys("memory", "system_uri", func(u string) (any, error) { return c.MemoryInventory(ctx, u) })
	case "Systems/GetBiosAttributes":
		return sys("bios_attribute", "system_uri", func(u string) (any, error) { return c.BiosAttributes(ctx, u) })
	case "Systems/GetBootOrder":
		return sys("boot_order", "system_uri", func(u string) (any, error) { return c.BootOrder(ctx, u) })
	case "Systems/GetStorageControllerInventory":
		return sys("storage_controller", "system_uri", func(u string) (any, error) { return c.StorageControllerInventory(ctx, u) })
	case "Systems/GetDiskInventory":
		return sys("disk", "system_uri", func(u string) (any, error) { return c.DiskInventory(ctx, u) })
	case "Systems/GetVolumeInventory":
		return sys("volume", "system_uri", func(u string) (any, error) { return c.VolumeInventory(ctx, u) })

	case "Chassis/GetChassisInventory":
		v, err := c.ChassisInventory(ctx)
		return list("chassis", v, err)
	case "Chassis/GetFanInventory":
		v, err := c.FanInventory(ctx)
		return list("fan", v, err)
	case "Chassis/GetChassisPower":
		v, err := c.ChassisPower(ctx)
		return list("chassis_power", v, err)
	case "Chassis/GetHealthReport":
		v, err := c.ChassisHealthReports(ctx)
		if err != nil {
			return "health_report", nil, err
		}
		entries := []any{}
		for _, p := range v {
			entries = append(entries, []any{map[string]any{"chassis_uri": p["chassis_uri"]}, p["health"]})
		}
		return "health_report", infoOK(entries), nil
	case "Chassis/GetChassisThermals":
		v, err := c.ChassisThermals(ctx)
		return list("thermals", v, err)
	case "Chassis/GetPsuInventory":
		v, err := c.PSUInventory(ctx)
		return list("psu", v, err)
	case "Chassis/GetHPEThermalConfig":
		v, err := c.HPEThermalConfig(ctx)
		if errors.Is(err, redfish.ErrUnsupported) {
			return "hpe_thermal_config", map[string]any{"ret": false}, nil
		}
		return list("hpe_thermal_config", v, err)
	case "Chassis/GetHPEFanPercentMin":
		v, err := c.HPEFanPercentMin(ctx)
		if errors.Is(err, redfish.ErrUnsupported) {
			return "hpe_fan_percent_min", map[string]any{"ret": false}, nil
		}
		return list("hpe_fan_percent_min", v, err)

	case "Accounts/ListUsers":
		v, err := c.ListUsers(ctx)
		return list("user", v, err)
	case "Accounts/GetAccountServiceConfig":
		v, err := c.AccountServiceConfig(ctx)
		return list("accountservice_config", v, err)
	case "Sessions/GetSessions":
		v, err := c.GetSessions(ctx)
		return list("session", v, err)

	case "Update/GetFirmwareInventory":
		v, err := c.FirmwareInventory(ctx)
		return list("firmware", v, err)
	case "Update/GetSoftwareInventory":
		v, err := c.SoftwareInventory(ctx)
		return list("software", v, err)
	case "Update/GetFirmwareUpdateCapabilities":
		v, err := c.FirmwareUpdateCapabilities(ctx)
		if err != nil {
			return "firmware_update_capabilities", nil, err
		}
		return "firmware_update_capabilities", map[string]any{"ret": true, "multipart_supported": v.MultipartSupported, "entries": v.Entries}, nil

	case "Manager/GetManagerInventory":
		return mgr("manager", "manager_uri", func(u string) (any, error) { return c.ManagerInventory(ctx, u) })
	case "Manager/GetNetworkProtocols":
		u, err := c.ResolveManager(ctx, "")
		if err != nil {
			return "network_protocols", nil, err
		}
		v, err := c.NetworkProtocols(ctx, u)
		return list("network_protocols", v, err)
	case "Manager/GetServiceIdentification":
		u, err := c.ResolveServiceIdentification(ctx, argString(args, "manager", ""))
		if err != nil {
			return "service_id", nil, err
		}
		s, err := c.ServiceIdentification(ctx, u)
		if err != nil {
			return "service_id", nil, err
		}
		return "service_id", map[string]any{"ret": true, "service_identification": s}, nil
	case "Manager/GetManagerNicInventory":
		return mgr("manager_nics", "resource_uri", func(u string) (any, error) { return c.ManagerNICInventory(ctx, u) })
	case "Manager/GetLogs":
		u, err := c.ResolveManager(ctx, "")
		if err != nil {
			return "log", nil, err
		}
		v, err := c.Logs(ctx, u, 0)
		if err != nil {
			return "log", nil, err
		}
		logs := []any{}
		for _, l := range v {
			logs = append(logs, map[string]any{"Description": l.Description, "Entries": l.Entries})
		}
		return "log", infoOK(logs), nil
	case "Manager/GetVirtualMedia":
		return mgr("virtual_media", "resource_uri", func(u string) (any, error) { return c.VirtualMedia(ctx, u) })
	case "Manager/GetHostInterfaces":
		v, err := c.HostInterfaces(ctx)
		if errors.Is(err, redfish.ErrNotFound) {
			return "host_interfaces", map[string]any{"ret": false, "msg": "No HostInterface objects found"}, nil
		}
		return list("host_interfaces", v, err)
	case "Manager/GetHealthReport":
		return mgr("health_report", "manager_uri", func(u string) (any, error) { return c.ManagerHealthReport(ctx, u) })
	}
	return "", nil, nil
}
