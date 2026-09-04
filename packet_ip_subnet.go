package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacketIpSubnet implements Ansible's `packet_ip_subnet`
// (community.general) module: assigns or unassigns an IPv4/IPv6 subnet
// to/from an Equinix Metal device — see packet_common.go's own doc
// comment for the `metal` CLI substitution shared by every packet_*
// module in this batch. Commands used: `ip assign -a <cidr> -d
// <device_id>` and `ip unassign -i <assignment_id>` — both confirmed
// real from metal-cli's own generated `metal_ip_assign.md`/
// `metal_ip_unassign.md` docs fetched during this batch's own
// research; existence-probing uses `device get -i <device_id>` (a
// device's own `ip_addresses` field lists its current assignments,
// each carrying the assignment's own `id` this port needs for
// unassign).
//
// Args: cidr (alias name, required — the exact subnet to assign/
// remove, matching real packet_ip_subnet.py's own semantics: this
// module does not create a new reservation, it assigns/removes a
// subnet already reserved for the project); device_id (or hostname,
// resolved via `device get -p <project_id>` filtered by hostname when
// device_id is not given directly); project_id (required when
// resolving by hostname); auth_token; state (present|absent, default
// present).
//
// present: Fail if device_id/hostname is not given (real
// packet_ip_subnet.py's own documented requirement: "With
// state=present, you must specify either hostname or device_id"). If
// the device's own current ip_addresses already contains an entry
// whose address+cidr matches, no-op; otherwise `ip assign`.
//
// absent: with device_id/hostname given, removes the matching
// assignment from that one device only; with NEITHER given, real
// packet_ip_subnet.py's own documented behavior is to remove the
// subnet from ANY device it is currently assigned to — this port does
// NOT implement that project-wide scan (it would need to list and
// probe every device in the project), a documented gap: absent with
// no device_id/hostname given Fails loud rather than silently doing
// nothing or guessing a device.
//
// Extra: unchanged from a bare Ok/Changed — no separate return value
// beyond changed/msg, matching real packet_ip_subnet.py's own
// documented RETURN VALUES (it declares none beyond Ansible's own
// standard changed/msg).
func modulePacketIpSubnet(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := metalRequireBinary(ctx, conn, "packet_ip_subnet"); !ok {
		return res, nil
	}
	authToken := argString(args, "auth_token", "")
	cidr, err := requireString(args, "cidr")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("packet_ip_subnet: state must be one of present, absent, got %q", state)
	}
	deviceID := argString(args, "device_id", "")
	hostname := argString(args, "hostname", "")
	projectID := argString(args, "project_id", "")

	if deviceID == "" && hostname != "" {
		if projectID == "" {
			return Result{}, errArg("packet_ip_subnet: project_id is required to resolve hostname to a device_id")
		}
		var listResp map[string]any
		lres, err := metalRunJSON(ctx, conn, authToken, &listResp, "device", "get", "-p", projectID)
		if err != nil {
			return Result{}, err
		}
		if lres.RC != 0 {
			return metalFail("packet_ip_subnet", "listing devices", lres), nil
		}
		match, found, ambiguous := metalFindByField(metalListArray(listResp), "hostname", hostname)
		if ambiguous {
			return Fail("packet_ip_subnet: more than one device matches hostname " + hostname), nil
		}
		if !found {
			return Fail("packet_ip_subnet: no device found with hostname " + hostname), nil
		}
		deviceID = fmt.Sprint(match["id"])
	}

	if state == "present" && deviceID == "" {
		return Fail("packet_ip_subnet: one of device_id, hostname is required for state=present"), nil
	}
	if state == "absent" && deviceID == "" {
		return Fail("packet_ip_subnet: this port requires device_id or hostname for state=absent — it does " +
			"not implement real packet_ip_subnet.py's own project-wide \"remove from any device\" scan; see " +
			"packet_ip_subnet.go's own doc comment"), nil
	}

	var device map[string]any
	dres, err := metalRunJSON(ctx, conn, authToken, &device, "device", "get", "-i", deviceID)
	if err != nil {
		return Result{}, err
	}
	if dres.RC != 0 {
		return metalFail("packet_ip_subnet", "reading device "+deviceID, dres), nil
	}
	assignmentID, assigned := packetFindIPAssignment(device, cidr)

	if state == "absent" {
		if !assigned {
			return Ok("packet_ip_subnet: " + cidr + " already not assigned to " + deviceID), nil
		}
		ures, err := metalRun(ctx, conn, authToken, "ip", "unassign", "-i", assignmentID)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return metalFail("packet_ip_subnet", "unassigning "+cidr, ures), nil
		}
		return Changed("packet_ip_subnet: " + cidr + " unassigned from " + deviceID), nil
	}

	if assigned {
		return Ok("packet_ip_subnet: " + cidr + " already assigned to " + deviceID), nil
	}
	ares, err := metalRun(ctx, conn, authToken, "ip", "assign", "-a", cidr, "-d", deviceID)
	if err != nil {
		return Result{}, err
	}
	if ares.RC != 0 {
		return metalFail("packet_ip_subnet", "assigning "+cidr, ares), nil
	}
	return Changed("packet_ip_subnet: " + cidr + " assigned to " + deviceID), nil
}

// packetFindIPAssignment scans device's own "ip_addresses" array
// (metal-cli's own device JSON shape) for an entry whose own "address"
// (optionally combined with "cidr") matches wantCIDR, returning that
// entry's own assignment id.
func packetFindIPAssignment(device map[string]any, wantCIDR string) (assignmentID string, found bool) {
	addrs, ok := device["ip_addresses"].([]any)
	if !ok {
		return "", false
	}
	for _, a := range addrs {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		addr := fmt.Sprint(am["address"])
		cidrLen := fmt.Sprint(am["cidr"])
		full := addr
		if cidrLen != "" && cidrLen != "<nil>" {
			full = addr + "/" + cidrLen
		}
		if full == wantCIDR || addr == wantCIDR {
			return fmt.Sprint(am["id"]), true
		}
	}
	return "", false
}
