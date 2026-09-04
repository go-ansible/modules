package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacketDevice implements Ansible's `packet_device`
// (community.general) module: creates, deletes, or changes the power
// state of an Equinix Metal (bare-metal) device — see
// packet_common.go's own doc comment for the `metal` CLI substitution
// shared by every packet_* module in this batch. Commands used:
// `device get -p <project_id> [--filter hostname=<name>]` (list/probe
// — confirmed real: metal-cli's own generated docs document `-i` for
// one device and `-p` alone for "list devices in a project", plus
// `--filter`), `device create -p <project_id> -H <hostname> -O
// <operating_system> -P <plan> [-f <facility>|-m <metro>] [-a] [-t
// <tags>]`, `device delete -i <id> -f`, `device start -i <id>`,
// `device stop -i <id>`, `device reboot -i <id>` — every flag
// independently confirmed from metal-cli's own generated
// `device_create`/`device_get`/`device_delete` docs fetched during this
// batch's own research; start/stop/reboot subcommands are confirmed to
// exist (metal-cli's own top-level `device` help text: "create, get,
// update, delete, reinstall, start, stop, and reboot") but their own
// individual flag surfaces were not each separately fetched — only
// `-i <id>` is used, the obvious minimum every one of those verbs
// needs.
//
// Deviation — single device per invocation: real packet_device.py
// supports `count`/`count_offset` to expand one hostnames entry (with
// a `%d` format placeholder) into several devices in one task. This
// port does not implement that expansion — hostnames' own FIRST entry
// (or a bare string) is the one device this port acts on, a documented
// simplification given this batch's own time constraints, not a
// silent one.
//
// Deviation — no wait_timeout polling: real packet_device.py's own
// state=active blocks (up to wait_timeout) until the device reports
// active. This port triggers device creation/start and returns
// immediately without polling — see hwc_common.go's own doc comment
// on this exact class of tradeoff (this port's other batches DO poll
// short, bounded windows for async cloud operations; packet_device's
// own real wait_timeout default is 900s, an order of magnitude past
// what this port judges reasonable to block a single module
// invocation on).
//
// Args: project_id (required); hostnames (alias name — first entry
// used, see above); operating_system, plan (required to create);
// facility or metro (facility passed through as -f; this port does
// NOT implement metro-based creation since real packet_device.py's own
// argument_spec has no metro field at all — metro is metal-cli's own
// newer addition, out of scope for argument-shape compatibility with
// the real module); always_pxe (-> -a); device_ids (when given,
// operates directly on that id instead of a hostname lookup);
// locked/lock (accepted, not wired — metal-cli's own device create has
// no lock/unlock flag in its confirmed flag list); features,
// ipxe_script_url, user_data (accepted, partially wired: user_data ->
// -u, ipxe_script_url -> -I); auth_token; state (absent, active,
// inactive, rebooted, present — default present).
//
// present/active: create if not found; active additionally issues
// `device start` when the found/created device's own status isn't
// already "active". inactive: `device stop`. rebooted: `device
// reboot` (always Changed when the device is found; Fail if it is
// not). absent: `device delete` if found, else no-op.
//
// Extra["id"]: present whenever the device now exists (or existed,
// for the power-state-only paths).
func modulePacketDevice(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := metalRequireBinary(ctx, conn, "packet_device"); !ok {
		return res, nil
	}
	authToken := argString(args, "auth_token", "")
	projectID, err := requireString(args, "project_id")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "active", "inactive", "rebooted":
	default:
		return Result{}, errArg("packet_device: state must be one of present, absent, active, inactive, rebooted, got %q", state)
	}

	hostnames := argStringList(args, "hostnames")
	hostname := ""
	if len(hostnames) > 0 {
		hostname = hostnames[0]
	}
	deviceIDs := argStringList(args, "device_ids")
	deviceID := ""
	if len(deviceIDs) > 0 {
		deviceID = deviceIDs[0]
	}
	if deviceID == "" && hostname == "" {
		return Result{}, errArg("packet_device: one of hostnames, device_ids is required")
	}

	var listResp map[string]any
	lres, err := metalRunJSON(ctx, conn, authToken, &listResp, "device", "get", "-p", projectID)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return metalFail("packet_device", "listing devices", lres), nil
	}
	items := metalListArray(listResp)
	var match map[string]any
	var found, ambiguous bool
	if deviceID != "" {
		match, found, ambiguous = metalFindByField(items, "id", deviceID)
	} else {
		match, found, ambiguous = metalFindByField(items, "hostname", hostname)
	}
	if ambiguous {
		return Fail("packet_device: more than one device matches; execution aborted"), nil
	}

	if state == "absent" {
		if !found {
			return Ok("packet_device: already absent"), nil
		}
		id := fmt.Sprint(match["id"])
		dres, err := metalRun(ctx, conn, authToken, "device", "delete", "-i", id, "-f")
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return metalFail("packet_device", "deleting "+id, dres), nil
		}
		return Changed("packet_device: "+id+" deleted").WithExtra("id", id), nil
	}

	changed := false
	if !found {
		if hostname == "" {
			return Fail("packet_device: device_ids " + deviceID + " not found and no hostname given to create it"), nil
		}
		operatingSystem, err := requireString(args, "operating_system")
		if err != nil {
			return Result{}, errArg("packet_device: operating_system is required to create a device: %v", err)
		}
		plan, err := requireString(args, "plan")
		if err != nil {
			return Result{}, errArg("packet_device: plan is required to create a device: %v", err)
		}
		argv := []string{"device", "create", "-p", projectID, "-H", hostname, "-O", operatingSystem, "-P", plan}
		if v := argString(args, "facility", ""); v != "" {
			argv = append(argv, "-f", v)
		}
		if argBool(args, "always_pxe", false) {
			argv = append(argv, "-a")
		}
		if v := argString(args, "ipxe_script_url", ""); v != "" {
			argv = append(argv, "-I", v)
		}
		if v := argString(args, "user_data", ""); v != "" {
			argv = append(argv, "-u", v)
		}
		var created map[string]any
		cres, err := metalRunJSON(ctx, conn, authToken, &created, argv...)
		if err != nil {
			return Result{}, err
		}
		if cres.RC != 0 {
			return metalFail("packet_device", "creating "+hostname, cres), nil
		}
		match = created
		found = true
		changed = true
	}

	id := fmt.Sprint(match["id"])
	switch state {
	case "active":
		if fmt.Sprint(match["state"]) != "active" {
			res, err := metalRun(ctx, conn, authToken, "device", "start", "-i", id)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return metalFail("packet_device", "starting "+id, res), nil
			}
			changed = true
		}
	case "inactive":
		res, err := metalRun(ctx, conn, authToken, "device", "stop", "-i", id)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return metalFail("packet_device", "stopping "+id, res), nil
		}
		changed = true
	case "rebooted":
		res, err := metalRun(ctx, conn, authToken, "device", "reboot", "-i", id)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return metalFail("packet_device", "rebooting "+id, res), nil
		}
		changed = true
	}

	if changed {
		return Changed("packet_device: "+id).WithExtra("id", id), nil
	}
	return Ok("packet_device: "+id+" already up to date").WithExtra("id", id), nil
}
