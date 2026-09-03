package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleNmcli implements (a subset of) Ansible's `nmcli`
// (community.general) module: creates/updates/removes a
// NetworkManager connection profile via the `nmcli` CLI.
//
// Real nmcli's own option surface is enormous — dozens of connection
// types (ethernet, bond, bridge, vlan, wifi, team, gsm, macvlan, vpn,
// ovs-*, wireguard, infiniband, ...) each with their own property
// subset, well over a hundred arguments in total. Reimplementing all
// of it was judged out of scope for this batch; this port instead
// covers the common/core case honestly:
//
// Args: conn_name (string, required); type (string, required — one of
// ethernet, generic, dummy, vlan, bond, bridge, team, bond-slave,
// bridge-slave, team-slave; any other real nmcli type — wifi, gsm,
// macvlan, vpn, ovs-*, wireguard, infiniband, and more — fails cleanly
// (Result{Failed:true}) naming what's unsupported, rather than silently
// misapplying ethernet-shaped handling to a type it doesn't fit);
// state (present|absent, required — matches real nmcli's own doc,
// which lists no default; state=up/down, real nmcli's own newer
// "activate/deactivate without touching other parameters" states, are
// NOT implemented here); ifname (string, optional); autoconnect (bool,
// default true); master, slave_type (bond|bridge|team|ovs-port|vrf);
// zone (string); mtu (int, ethernet type only — real nmcli documents
// mtu as usable for Team/VLAN/Ethernet; this port only implements the
// ethernet case, `802-3-ethernet.mtu`); mode (string, bond type only —
// applied as `bond.options mode=<mode>`); vlanid (int), vlandev
// (string, vlan type only — `802-1Q.id`/`802-1Q.parent`); ip4, gw4,
// method4 (auto|link-local|manual|shared|disabled), dns4, dns4_search
// ([]string); ip6, gw6, method6 (same shape, IPv6) — mapped onto
// nmcli's own `ipv4.addresses`/`ipv4.gateway`/`ipv4.method`/
// `ipv4.dns`/`ipv4.dns-search` and their `ipv6.*` equivalents (a list
// argument is joined with `,`, matching nmcli's own list-property
// syntax).
//
// NOT implemented: wifi/wifi_sec, gsm, macvlan, sriov, routes4/routes6
// (and their _extended forms), routing_rules4/6, infiniband_mac,
// ip_tunnel_*, vxlan_*, wireguard, runner*, addr_gen_mode6, and every
// other property real nmcli documents beyond the set listed above —
// this is a real, substantial narrowing of real nmcli's own option
// surface, not a hidden gap: an argument this port doesn't recognize
// is simply never read (matching this package's general convention of
// only reading the arguments a module actually implements), which
// means a playbook relying on one of them will see no effect and no
// error — check this doc comment's own arg list before assuming a
// property is covered.
//
// Idempotency: `nmcli -g connection.id connection show <conn_name>`
// determines whether the profile already exists. state=absent deletes
// it if present (`nmcli connection delete <conn_name>`), else no-op.
// state=present, profile missing: `nmcli connection add type <type>
// con-name <conn_name> [ifname <ifname>] <property> <value>...` for
// every property this port supports that was actually given. state=
// present, profile existing: each supported property that was given is
// queried individually via `nmcli -g <property> connection show
// <conn_name>` and compared (as text) against its desired value;
// `nmcli connection modify <conn_name> <property> <value>...` is run
// with only the differing ones, and Changed is reported only if that
// set is non-empty — real per-property idempotency, not the "always
// act, report changed unconditionally" shortcut this package uses
// elsewhere (e.g. sysvinit.go's own enable/disable) for cases where a
// cheap comparison isn't available; here it is, via `-g`.
func moduleNmcli(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	connName, err := requireString(args, "conn_name")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("nmcli: state must be present or absent, got %q", state)
	}

	exists, err := nmcliConnectionExists(ctx, conn, connName)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(connName + " already absent"), nil
		}
		if _, err := run(ctx, conn, "nmcli connection delete "+shellQuote(connName)); err != nil {
			return Result{}, err
		}
		return Changed(connName + " deleted"), nil
	}

	nmType, err := requireString(args, "type")
	if err != nil {
		return Result{}, err
	}
	validTypes := map[string]bool{
		"ethernet": true, "generic": true, "dummy": true, "vlan": true,
		"bond": true, "bridge": true, "team": true,
		"bond-slave": true, "bridge-slave": true, "team-slave": true,
	}
	if !validTypes[nmType] {
		return Fail("nmcli: type " + nmType + " is not supported by this port (see moduleNmcli's own doc comment " +
			"for the list of implemented types)"), nil
	}

	fields, err := nmcliFields(args, nmType)
	if err != nil {
		return Result{}, err
	}

	if !exists {
		cmd := "nmcli connection add type " + nmType + " con-name " + shellQuote(connName)
		if ifname := argString(args, "ifname", ""); ifname != "" {
			cmd += " ifname " + shellQuote(ifname)
		}
		for _, f := range fields {
			cmd += " " + f.name + " " + shellQuote(f.value)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(connName + " created"), nil
	}

	var toModify []nmcliField
	for _, f := range fields {
		current, err := run(ctx, conn, "nmcli -g "+f.name+" connection show "+shellQuote(connName))
		if err != nil {
			toModify = append(toModify, f)
			continue
		}
		if current != f.value {
			toModify = append(toModify, f)
		}
	}
	if len(toModify) == 0 {
		return Ok(connName + " already up to date"), nil
	}
	cmd := "nmcli connection modify " + shellQuote(connName)
	for _, f := range toModify {
		cmd += " " + f.name + " " + shellQuote(f.value)
	}
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(connName + " updated"), nil
}

type nmcliField struct{ name, value string }

// nmcliFields builds the nmcli dotted-property/value pairs for every
// supported argument actually given in args, gated by nmType where a
// property only applies to one connection type (mtu/ethernet,
// mode/bond, vlanid+vlandev/vlan).
func nmcliFields(args map[string]any, nmType string) ([]nmcliField, error) {
	var fields []nmcliField
	add := func(name, value string) {
		if value != "" {
			fields = append(fields, nmcliField{name, value})
		}
	}

	fields = append(fields, nmcliField{"connection.autoconnect", nmcliYesNo(argBool(args, "autoconnect", true))})
	add("connection.zone", argString(args, "zone", ""))
	add("master", argString(args, "master", ""))
	add("connection.slave-type", argString(args, "slave_type", ""))

	add("ipv4.addresses", strings.Join(argStringList(args, "ip4"), ","))
	add("ipv4.gateway", argString(args, "gw4", ""))
	if method4 := argString(args, "method4", ""); method4 != "" {
		validMethod4 := map[string]bool{"auto": true, "link-local": true, "manual": true, "shared": true, "disabled": true}
		if !validMethod4[method4] {
			return nil, errArg("nmcli: method4 must be one of auto, link-local, manual, shared, disabled, got %q", method4)
		}
		add("ipv4.method", method4)
	}
	add("ipv4.dns", strings.Join(argStringList(args, "dns4"), ","))
	add("ipv4.dns-search", strings.Join(argStringList(args, "dns4_search"), ","))

	add("ipv6.addresses", strings.Join(argStringList(args, "ip6"), ","))
	add("ipv6.gateway", argString(args, "gw6", ""))
	add("ipv6.method", argString(args, "method6", ""))

	if nmType == "ethernet" {
		if _, ok := args["mtu"]; ok {
			add("802-3-ethernet.mtu", strconv.Itoa(argInt(args, "mtu", 0)))
		}
	}
	if nmType == "bond" {
		add("bond.options", modeOption(argString(args, "mode", "")))
	}
	if nmType == "vlan" {
		if _, ok := args["vlanid"]; ok {
			add("802-1Q.id", strconv.Itoa(argInt(args, "vlanid", 0)))
		}
		add("802-1Q.parent", argString(args, "vlandev", ""))
	}

	return fields, nil
}

func modeOption(mode string) string {
	if mode == "" {
		return ""
	}
	return "mode=" + mode
}

func nmcliYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// nmcliConnectionExists reports whether conn_name is already a known
// NetworkManager connection profile.
func nmcliConnectionExists(ctx context.Context, conn remoteexec.Connection, connName string) (bool, error) {
	res, err := runStatus(ctx, conn, "nmcli -g connection.id connection show "+shellQuote(connName))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
