package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewaySecurityGroupRule implements Ansible's
// `scaleway_security_group_rule` (community.general) module: creates/
// deletes a rule within a Scaleway instance security group, via `scw
// instance security-group create-rule/list-rules/delete-rule` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI, and for the region->zone mapping (scwZone) used below.
//
// Args: state (present|absent, default present); region (required);
// protocol (required, TCP|UDP|ICMP); port (required KEY — but its
// VALUE may be an explicit null/absent, meaning "all ports", matching
// real scaleway_security_group_rule's own `port=dict(type="int",
// required=True)` combined with its own doc text "Port related to the
// rule, null value for all the ports" — Ansible's `required` only means
// the key must be given, not that its value is non-null, so an explicit
// `port: null` in a playbook satisfies it); ip_range (default
// 0.0.0.0/0); direction (required, inbound|outbound); action (required,
// accept|drop); security_group (required, the security group's own
// ID).
//
// Matching: real get_sgr_from_api finds an existing rule by comparing
// (ip_range, dest_port_from, direction, action, protocol) as a tuple —
// this port replicates that exact 5-field match against `scw instance
// security-group list-rules security-group-id=<security_group>
// zone=<zone> -o json`'s own decoded rule objects.
//
// present: match found -> Changed=false. Not found -> `scw instance
// security-group create-rule security-group-id=<security_group>
// protocol=<protocol> direction=<direction> action=<action>
// ip-range=<ip_range> [dest-port-from=<port>] zone=<zone>`,
// Changed=true (dest-port-from omitted entirely when port is null/all-
// ports, matching real payload_from_object's own "drop None values"
// behavior).
//
// absent: match found -> `scw instance security-group delete-rule
// security-group-id=<security_group> security-group-rule-id=<id>
// zone=<zone>`, Changed=true; not found -> Changed=false.
//
// Extra["security_group_rule"]: the matched/created rule object,
// decoded directly from `scw`'s own JSON output (see
// scaleway_common.go's own "Output shape" caveat).
func moduleScalewaySecurityGroupRule(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_security_group_rule"); !ok {
		return res, nil
	}
	region, err := requireString(args, "region")
	if err != nil {
		return Result{}, err
	}
	zone, err := scwZone(region)
	if err != nil {
		return Result{}, err
	}
	protocol, err := requireString(args, "protocol")
	if err != nil {
		return Result{}, err
	}
	if protocol != "TCP" && protocol != "UDP" && protocol != "ICMP" {
		return Result{}, errArg("scaleway_security_group_rule: protocol must be TCP, UDP, or ICMP, got %q", protocol)
	}
	direction, err := requireString(args, "direction")
	if err != nil {
		return Result{}, err
	}
	if direction != "inbound" && direction != "outbound" {
		return Result{}, errArg("scaleway_security_group_rule: direction must be inbound or outbound, got %q", direction)
	}
	action, err := requireString(args, "action")
	if err != nil {
		return Result{}, err
	}
	if action != "accept" && action != "drop" {
		return Result{}, errArg("scaleway_security_group_rule: action must be accept or drop, got %q", action)
	}
	securityGroup, err := requireString(args, "security_group")
	if err != nil {
		return Result{}, err
	}
	ipRange := argString(args, "ip_range", "0.0.0.0/0")
	if _, ok := args["port"]; !ok {
		return Result{}, errArg("scaleway_security_group_rule: missing required argument: port (pass null for all ports)")
	}
	var port *int
	if v := args["port"]; v != nil {
		n := argInt(args, "port", 0)
		port = &n
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_security_group_rule: state must be present or absent, got %q", state)
	}

	listRes, err := scwRunJSON(ctx, conn, "instance", "security-group", "list-rules", "security-group-id="+securityGroup, "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if listRes.RC != 0 {
		return Fail("scaleway_security_group_rule: failed to list rules for security group " + securityGroup + ": " + scwErrMsg(listRes)), nil
	}
	var rules []map[string]any
	if derr := scwDecode(listRes.Stdout, &rules); derr != nil {
		return Result{}, derr
	}
	var match map[string]any
	for _, r := range rules {
		if scwSGRuleStr(r, "ip_range") == ipRange &&
			scwSGRuleStr(r, "direction") == direction &&
			scwSGRuleStr(r, "action") == action &&
			scwSGRuleStr(r, "protocol") == protocol &&
			scwSGRulePortEqual(r, port) {
			match = r
			break
		}
	}

	if state == "absent" {
		if match == nil {
			return Ok(""), nil
		}
		id := scwSGRuleStr(match, "id")
		delRes, err := scwRun(ctx, conn, "instance", "security-group", "delete-rule", "security-group-id="+securityGroup, "security-group-rule-id="+id, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_security_group_rule: failed to delete rule " + id + ": " + scwErrMsg(delRes)), nil
		}
		return Changed(""), nil
	}

	if match != nil {
		res := Result{Changed: false}
		return res.WithExtra("security_group_rule", match), nil
	}

	createArgs := []string{
		"instance", "security-group", "create-rule",
		"security-group-id=" + securityGroup,
		"protocol=" + protocol,
		"direction=" + direction,
		"action=" + action,
		"ip-range=" + ipRange,
	}
	if port != nil {
		createArgs = append(createArgs, fmt.Sprintf("dest-port-from=%d", *port))
	}
	createArgs = append(createArgs, "zone="+zone)
	createRes, err := scwRunJSON(ctx, conn, createArgs...)
	if err != nil {
		return Result{}, err
	}
	if createRes.RC != 0 {
		return Fail("scaleway_security_group_rule: failed to create rule: " + scwErrMsg(createRes)), nil
	}
	var created map[string]any
	if derr := scwDecode(createRes.Stdout, &created); derr != nil {
		return Result{}, derr
	}
	return Changed("").WithExtra("security_group_rule", created), nil
}

func scwSGRuleStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// scwSGRulePortEqual compares a decoded rule's "dest_port_from" field
// (a JSON number decoded as float64, or absent/null for "all ports")
// against port (nil meaning the same "all ports").
func scwSGRulePortEqual(r map[string]any, port *int) bool {
	v, ok := r["dest_port_from"]
	if !ok || v == nil {
		return port == nil
	}
	if port == nil {
		return false
	}
	n, ok := v.(float64)
	if !ok {
		return false
	}
	return int(n) == *port
}
