package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

var iloRedfishConfigCategories = map[string][]string{
	"Manager": {"SetTimeZone", "SetDNSserver", "SetDomainName", "SetNTPServers", "SetWINSReg"},
}

// moduleIloRedfishConfig implements Ansible's `ilo_redfish_config`
// module: sets one iLO Manager network/time attribute per invocation.
// Real ilo_redfish_config.py's own five commands (read from
// module_utils/_ilo_redfish_utils.py before writing this file) each PATCH
// a specific iLO Manager sub-resource:
//
//	SetTimeZone      PATCH Managers/<id>/DateTime/           {"TimeZone": {"Index": <looked-up index>}}
//	SetNTPServers    PATCH Managers/<id>/DateTime/           {"<attribute_name>": [ip1, ip2]} (also disables DHCP-supplied NTP first)
//	SetDNSserver     PATCH <manager's own ethernet NIC URI>  {"Oem": {"Hpe": {"IPv4": {"<attribute_name>": [ip1,ip2,ip3]}}}}
//	SetDomainName    PATCH <manager's own ethernet NIC URI>  {"Oem": {"Hpe": {"<attribute_name>": "<value>"}}} (also disables DHCP-supplied domain name first)
//	SetWINSReg       PATCH <manager's own ethernet NIC URI>  {"Oem": {"Hpe": {"IPv4": {"<attribute_name>": false}}}}
//
// # ilorest mapping
//
// Reproduced here via iloRawGet/iloRawPatch (see ilo_common.go's own doc
// comment on why ilorest, unlike racadm/OneCli, genuinely supports this
// kind of arbitrary raw GET/PATCH). The "manager's own ethernet NIC URI"
// this port resolves via iloManagerEthernetURI below (GET the Manager
// resource's own EthernetInterfaces collection, take its first member) —
// a good-faith, generically-correct reproduction of "the manager's own
// primary NIC", not a byte-for-byte copy of HPE's own private
// get_manager_ethernet_uri() helper (whose own source is internal to
// _ilo_redfish_utils.py and was not fully available to this port beyond
// its call sites read from the modules that use it); on an iLO with more
// than one Manager-owned NIC this could pick a different one than
// upstream's own helper would, an honestly-disclosed limitation.
//
// Args: category (required, must be "Manager"); command (required list,
// each one of the five above); attribute_name (required) — the target
// resource's own field name to set (e.g. "TimeZone", "StaticNTPServers",
// "WINSRegistration" — matching real ilo_redfish_config's own EXAMPLES);
// attribute_value (string, optional — not needed for SetWINSReg, which
// always disables registration). baseuri/username/password/auth_token/
// timeout accepted for shape compatibility, NO EFFECT.
func moduleIloRedfishConfig(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	category, err := requireString(args, "category")
	if err != nil {
		return Result{}, err
	}
	commands := argStringList(args, "command")
	if len(commands) == 0 {
		return Result{}, errArg("ilo_redfish_config: missing required argument: command")
	}
	attrName, err := requireString(args, "attribute_name")
	if err != nil {
		return Result{}, err
	}
	attrValue := argString(args, "attribute_value", "")

	if res, ok := redfishCheckCategory("ilo_redfish_config", category, iloRedfishConfigCategories); !ok {
		return res, nil
	}
	if res, ok := redfishCheckCommands("ilo_redfish_config", category, commands, iloRedfishConfigCategories); !ok {
		return res, nil
	}
	if res, ok := iloRequireBinary(ctx, conn, "ilo_redfish_config"); !ok {
		return res, nil
	}

	managerURI, err := iloManagerURI(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if managerURI == "" {
		return Fail("ilo_redfish_config: could not find a Manager resource"), nil
	}

	result := map[string]any{}
	changed := false
	for _, command := range commands {
		var res map[string]any
		var err error
		switch command {
		case "SetTimeZone":
			res, err = iloSetTimeZone(ctx, conn, managerURI, attrName, attrValue)
		case "SetNTPServers":
			res, err = iloSetNTPServers(ctx, conn, managerURI, attrName, attrValue)
		case "SetDNSserver":
			res, err = iloSetDNSServer(ctx, conn, managerURI, attrName, attrValue)
		case "SetDomainName":
			res, err = iloSetDomainName(ctx, conn, managerURI, attrName, attrValue)
		case "SetWINSReg":
			res, err = iloSetWINSReg(ctx, conn, managerURI, attrName)
		}
		if err != nil {
			return Result{}, err
		}
		result[command] = res
		if c, _ := res["changed"].(bool); c {
			changed = true
		}
		if ok, _ := res["ret"].(bool); !ok {
			return Fail(fmt.Sprint(res["msg"])), nil
		}
	}

	out := Ok("")
	out.Changed = changed
	return out.WithExtra("ilo_redfish_config", result), nil
}

// iloManagerURI resolves the iLO's own Manager resource URI (the first
// member of /redfish/v1/Managers/).
func iloManagerURI(ctx context.Context, conn remoteexec.Connection) (string, error) {
	var coll struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	res, err := iloRawGet(ctx, conn, "/redfish/v1/Managers/", &coll)
	if err != nil {
		return "", err
	}
	if res.RC != 0 || len(coll.Members) == 0 {
		return "", nil
	}
	return coll.Members[0].ODataID, nil
}

// iloManagerEthernetURI resolves the manager's own first EthernetInterfaces
// member — see this file's own doc comment on this being a good-faith,
// not byte-verified, reproduction of get_manager_ethernet_uri().
func iloManagerEthernetURI(ctx context.Context, conn remoteexec.Connection, managerURI string) (string, error) {
	var mgr struct {
		EthernetInterfaces struct {
			ODataID string `json:"@odata.id"`
		} `json:"EthernetInterfaces"`
	}
	res, err := iloRawGet(ctx, conn, managerURI, &mgr)
	if err != nil {
		return "", err
	}
	if res.RC != 0 || mgr.EthernetInterfaces.ODataID == "" {
		return "", nil
	}
	var coll struct {
		Members []struct {
			ODataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	res, err = iloRawGet(ctx, conn, mgr.EthernetInterfaces.ODataID, &coll)
	if err != nil {
		return "", err
	}
	if res.RC != 0 || len(coll.Members) == 0 {
		return "", nil
	}
	return coll.Members[0].ODataID, nil
}

func iloSetTimeZone(ctx context.Context, conn remoteexec.Connection, managerURI, attrName, attrValue string) (map[string]any, error) {
	uri := managerURI + "DateTime/"
	var data map[string]any
	res, err := iloRawGet(ctx, conn, uri, &data)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(res)}, nil
	}
	if _, ok := data[attrName]; !ok {
		return map[string]any{"ret": false, "changed": false, "msg": fmt.Sprintf("Key %s not found", attrName)}, nil
	}
	index := ""
	if list, ok := data["TimeZoneList"].([]any); ok {
		for _, e := range list {
			tz, _ := e.(map[string]any)
			if name, _ := tz["Name"].(string); strings.Contains(name, attrValue) {
				index = fmt.Sprint(tz["Index"])
				break
			}
		}
	}
	payload := map[string]any{attrName: map[string]any{"Index": index}}
	pres, err := iloRawPatch(ctx, conn, uri, payload)
	if err != nil {
		return nil, err
	}
	if pres.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
	}
	return map[string]any{"ret": true, "changed": true, "msg": "Modified " + attrName}, nil
}

func iloSetNTPServers(ctx context.Context, conn remoteexec.Connection, managerURI, attrName, attrValue string) (map[string]any, error) {
	ethURI, err := iloManagerEthernetURI(ctx, conn, managerURI)
	if err != nil {
		return nil, err
	}
	if ethURI == "" {
		return map[string]any{"ret": false, "msg": "could not find manager's own ethernet interface"}, nil
	}
	var eth map[string]any
	res, err := iloRawGet(ctx, conn, ethURI, &eth)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(res)}, nil
	}
	if dhcpv4, ok := eth["DHCPv4"].(map[string]any); ok {
		if use, _ := dhcpv4["UseNTPServers"].(bool); use {
			if pres, err := iloRawPatch(ctx, conn, ethURI, map[string]any{"DHCPv4": map[string]any{"UseNTPServers": false}}); err != nil {
				return nil, err
			} else if pres.RC != 0 {
				return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
			}
		}
	}
	if dhcpv6, ok := eth["DHCPv6"].(map[string]any); ok {
		if use, _ := dhcpv6["UseNTPServers"].(bool); use {
			if pres, err := iloRawPatch(ctx, conn, ethURI, map[string]any{"DHCPv6": map[string]any{"UseNTPServers": false}}); err != nil {
				return nil, err
			} else if pres.RC != 0 {
				return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
			}
		}
	}

	ips := strings.Fields(attrValue)
	if len(ips) > 2 {
		return map[string]any{"ret": false, "changed": false, "msg": "More than 2 NTP Servers mentioned"}, nil
	}
	for len(ips) < 2 {
		ips = append(ips, "0.0.0.0")
	}
	dtURI := managerURI + "DateTime"
	payload := map[string]any{attrName: ips}
	pres, err := iloRawPatch(ctx, conn, dtURI, payload)
	if err != nil {
		return nil, err
	}
	if pres.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
	}
	return map[string]any{"ret": true, "changed": true, "msg": "Modified " + attrName}, nil
}

func iloSetDNSServer(ctx context.Context, conn remoteexec.Connection, managerURI, attrName, attrValue string) (map[string]any, error) {
	ethURI, err := iloManagerEthernetURI(ctx, conn, managerURI)
	if err != nil {
		return nil, err
	}
	if ethURI == "" {
		return map[string]any{"ret": false, "msg": "could not find manager's own ethernet interface"}, nil
	}
	ips := strings.Fields(attrValue)
	if len(ips) > 3 {
		return map[string]any{"ret": false, "changed": false, "msg": "More than 3 DNS Servers mentioned"}, nil
	}
	for len(ips) < 3 {
		ips = append(ips, "0.0.0.0")
	}
	payload := map[string]any{"Oem": map[string]any{"Hpe": map[string]any{"IPv4": map[string]any{attrName: ips}}}}
	pres, err := iloRawPatch(ctx, conn, ethURI, payload)
	if err != nil {
		return nil, err
	}
	if pres.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
	}
	return map[string]any{"ret": true, "changed": true, "msg": "Modified " + attrName}, nil
}

func iloSetDomainName(ctx context.Context, conn remoteexec.Connection, managerURI, attrName, attrValue string) (map[string]any, error) {
	ethURI, err := iloManagerEthernetURI(ctx, conn, managerURI)
	if err != nil {
		return nil, err
	}
	if ethURI == "" {
		return map[string]any{"ret": false, "msg": "could not find manager's own ethernet interface"}, nil
	}
	var eth map[string]any
	res, err := iloRawGet(ctx, conn, ethURI, &eth)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(res)}, nil
	}
	if dhcpv4, ok := eth["DHCPv4"].(map[string]any); ok {
		if use, _ := dhcpv4["UseDomainName"].(bool); use {
			if pres, err := iloRawPatch(ctx, conn, ethURI, map[string]any{"DHCPv4": map[string]any{"UseDomainName": false}}); err != nil {
				return nil, err
			} else if pres.RC != 0 {
				return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
			}
		}
	}
	if dhcpv6, ok := eth["DHCPv6"].(map[string]any); ok {
		if use, _ := dhcpv6["UseDomainName"].(bool); use {
			if pres, err := iloRawPatch(ctx, conn, ethURI, map[string]any{"DHCPv6": map[string]any{"UseDomainName": false}}); err != nil {
				return nil, err
			} else if pres.RC != 0 {
				return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
			}
		}
	}
	payload := map[string]any{"Oem": map[string]any{"Hpe": map[string]any{attrName: attrValue}}}
	pres, err := iloRawPatch(ctx, conn, ethURI, payload)
	if err != nil {
		return nil, err
	}
	if pres.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
	}
	return map[string]any{"ret": true, "changed": true, "msg": "Modified " + attrName}, nil
}

func iloSetWINSReg(ctx context.Context, conn remoteexec.Connection, managerURI, attrName string) (map[string]any, error) {
	ethURI, err := iloManagerEthernetURI(ctx, conn, managerURI)
	if err != nil {
		return nil, err
	}
	if ethURI == "" {
		return map[string]any{"ret": false, "msg": "could not find manager's own ethernet interface"}, nil
	}
	payload := map[string]any{"Oem": map[string]any{"Hpe": map[string]any{"IPv4": map[string]any{attrName: false}}}}
	pres, err := iloRawPatch(ctx, conn, ethURI, payload)
	if err != nil {
		return nil, err
	}
	if pres.RC != 0 {
		return map[string]any{"ret": false, "msg": iloErrMsg(pres)}, nil
	}
	return map[string]any{"ret": true, "changed": true, "msg": "Modified " + attrName}, nil
}
