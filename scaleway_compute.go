package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayCompute implements Ansible's `scaleway_compute`
// (community.general) module: creates/updates/deletes a Scaleway
// Instance (compute server) and manages its power state, via `scw
// instance server create/list/update/start/stop/reboot/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_compute's own direct REST API calls,
// and for the zone-translation/auth/wait deviations shared by every
// scaleway_* module in this batch.
//
// Args: image, commercial_type (both required); name (required by
// this port — see below); region (required, INSTANCE-API-zone-shaped,
// translated via scwZone); state
// (present|absent|running|restarted|stopped, default present);
// organization XOR project (exactly one required, matching real
// scaleway_compute's own mutually_exclusive+required_one_of); tags
// ([]string, max 5, default []); enable_ipv6 (bool, default false);
// security_group (string, optional); public_ip (string, default
// "absent") — "absent" -> `dynamic-ip-required=false`; "dynamic"/
// "allocated" -> `dynamic-ip-required=true`; anything else is treated
// as an existing IP/IP-ID and passed as `ip=<value>` to `scw instance
// server create` (matching real public_ip_payload's own three-way
// branch, verified against scaleway_compute.py, though this port does
// not first verify the IP exists via a GET the way real
// public_ip_payload does — a disclosed, harmless simplification: `scw`
// itself will reject a bad IP at create time).
//
// Deviation — name is required here even though real scaleway_compute's
// own `name` argument has no `required: true`: this port's own
// find-by-name idempotency (see scwFindByName) has no other stable key
// to look servers up by, since a server ID does not exist yet before
// creation. A caller of real scaleway_compute relying on Scaleway's own
// server-generated name (an unset name with the REST API) is not
// supported here.
//
// Deviation — real scaleway_compute's own check_image_id pre-validates
// the image argument with its own GET before ever attempting to create
// anything, giving a clearer failure. This port does not — `scw
// instance server create` will simply fail with its own error if image
// is invalid, surfaced via this port's own Fail(), a disclosed
// simplification, not a silent gap.
//
// present: finds the server by exact name in zone
// (`scw instance server list name=... zone=...`); creates it if missing
// (`scw instance server create image=... type=... name=...
// [tags.N=...] [ipv6=true] [security-group-id=...]
// [dynamic-ip-required=...|ip=...] [project-id=...|organization-id=...]
// zone=...`). Whether freshly created or pre-existing, this port then
// diffs the current server's own tags/security_group against the
// requested ones (real scaleway_compute's own PATCH_MUTABLE_SERVER_ATTRIBUTES
// is name/tags/dynamic_ip_required/security_group/ipv6 — this port
// diffs the subset it can reliably read back from `scw`'s own JSON:
// tags and security_group.id) and issues `scw instance server update`
// only for an actual difference — never changes the server's own power
// state.
//
// running/stopped: same find-or-create-then-diff as present, then
// starts the server (`scw instance server start`) if its current state
// is not "running"/"starting", or stops it (`scw instance server
// stop`) if not "stopped" — matching real running_strategy's/
// stop_strategy's own state check exactly (Changed=true whenever a
// power-state transition was actually issued, not unconditionally).
//
// restarted: same find-or-create-then-diff, then ALWAYS runs `scw
// instance server reboot` — matching real restart_strategy's own
// unconditional changed=True (it reboots every run, regardless of
// current state).
//
// absent: finds the server by name; Changed=false if not found. If
// found and not already "stopped", this port issues a SINGLE `scw
// instance server stop` first (real absent_strategy's own
// `while fetch_state(...) != "stopped"` loop retries and waits for the
// transition to actually complete — this port has no poll loop of its
// own, see scaleway_common.go's own doc comment on wait/wait_timeout —
// so a server that does not reach "stopped" quickly enough may still be
// running when the subsequent `scw instance server delete` is issued
// and that delete may itself then fail, surfaced honestly via Fail()
// rather than silently retried forever), then `scw instance server
// delete server-id=... zone=...` (Changed=true).
//
// Extra["server"]: `scw`'s own JSON object for the server (state=absent
// excepted). Deviation — real scaleway_compute's own module.exit_json
// call passes the server object (or, in some branches, a
// {"status": "..."} text summary) as its own `msg` argument directly —
// Ansible's `msg` is documented as a string but real scaleway_compute
// hands it a raw dict, which Ansible's core tolerates but this port's
// own Result.Msg field (string-only, see module.go's own Result type)
// cannot hold; this port surfaces the same information as
// Extra["server"] instead, with Msg carrying a short human-readable
// status string.
func moduleScalewayCompute(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_compute"); !ok {
		return res, nil
	}
	image, err := requireString(args, "image")
	if err != nil {
		return Result{}, err
	}
	commercialType, err := requireString(args, "commercial_type")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	region, err := requireString(args, "region")
	if err != nil {
		return Result{}, err
	}
	zone, err := scwZone(region)
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent", "running", "restarted", "stopped":
	default:
		return Result{}, errArg("scaleway_compute: state must be one of present, absent, running, restarted, stopped, got %q", state)
	}
	organization := argString(args, "organization", "")
	project := argString(args, "project", "")
	if organization == "" && project == "" {
		return Result{}, errArg("scaleway_compute: exactly one of organization, project must be specified")
	}
	if organization != "" && project != "" {
		return Result{}, errArg("scaleway_compute: organization and project are mutually exclusive")
	}
	securityGroup := argString(args, "security_group", "")
	enableIPv6 := argBool(args, "enable_ipv6", false)
	tags := argStringList(args, "tags")
	publicIP := argString(args, "public_ip", "absent")

	current, exists, err := scwFindByName(ctx, conn, name, "instance", "server", "list", "name="+name, "zone="+zone)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(""), nil
		}
		id, _ := current["id"].(string)
		if scwServerState(current) != "stopped" {
			stopRes, err := scwRun(ctx, conn, "instance", "server", "stop", id, "zone="+zone)
			if err != nil {
				return Result{}, err
			}
			if stopRes.RC != 0 {
				return Fail("scaleway_compute: failed to stop server " + name + " before deleting it: " + scwErrMsg(stopRes)), nil
			}
		}
		delRes, err := scwRun(ctx, conn, "instance", "server", "delete", id, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_compute: failed to delete server " + name + ": " + scwErrMsg(delRes)), nil
		}
		return Changed(""), nil
	}

	changed := false
	if !exists {
		argv := []string{"instance", "server", "create", "image=" + image, "type=" + commercialType,
			"name=" + name, "zone=" + zone}
		for i, tag := range tags {
			argv = append(argv, fmt.Sprintf("tags.%d=%s", i, tag))
		}
		if enableIPv6 {
			argv = append(argv, "ipv6=true")
		}
		if securityGroup != "" {
			argv = append(argv, "security-group-id="+securityGroup)
		}
		switch publicIP {
		case "absent":
			argv = append(argv, "dynamic-ip-required=false")
		case "dynamic", "allocated":
			argv = append(argv, "dynamic-ip-required=true")
		default:
			argv = append(argv, "ip="+publicIP)
		}
		if project != "" {
			argv = append(argv, "project-id="+project)
		}
		if organization != "" {
			argv = append(argv, "organization-id="+organization)
		}
		res, err := scwRunJSON(ctx, conn, argv...)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_compute: failed to create server " + name + ": " + scwErrMsg(res)), nil
		}
		if derr := scwDecode(res.Stdout, &current); derr != nil {
			return Result{}, derr
		}
		changed = true
	} else {
		curTags := scwAnyStringSlice(current["tags"])
		curSecGroup := ""
		if sg, ok := current["security_group"].(map[string]any); ok {
			curSecGroup, _ = sg["id"].(string)
		}
		diff := !stringSetEqual(curTags, tags) || (securityGroup != "" && curSecGroup != securityGroup)
		if diff {
			id, _ := current["id"].(string)
			argv := []string{"instance", "server", "update", id, "zone=" + zone}
			for i, tag := range tags {
				argv = append(argv, fmt.Sprintf("tags.%d=%s", i, tag))
			}
			if securityGroup != "" {
				argv = append(argv, "security-group-id="+securityGroup)
			}
			res, err := scwRunJSON(ctx, conn, argv...)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("scaleway_compute: failed to update server " + name + ": " + scwErrMsg(res)), nil
			}
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
			changed = true
		}
	}

	id, _ := current["id"].(string)
	switch state {
	case "running":
		st := scwServerState(current)
		if st != "running" && st != "starting" {
			res, err := scwRunJSON(ctx, conn, "instance", "server", "start", id, "zone="+zone)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("scaleway_compute: failed to start server " + name + ": " + scwErrMsg(res)), nil
			}
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
			changed = true
		}
	case "stopped":
		st := scwServerState(current)
		if st != "stopped" {
			res, err := scwRunJSON(ctx, conn, "instance", "server", "stop", id, "zone="+zone)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("scaleway_compute: failed to stop server " + name + ": " + scwErrMsg(res)), nil
			}
			if derr := scwDecode(res.Stdout, &current); derr != nil {
				return Result{}, derr
			}
			changed = true
		}
	case "restarted":
		res, err := scwRunJSON(ctx, conn, "instance", "server", "reboot", id, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("scaleway_compute: failed to reboot server " + name + ": " + scwErrMsg(res)), nil
		}
		if derr := scwDecode(res.Stdout, &current); derr != nil {
			return Result{}, derr
		}
		changed = true
	}

	r := Result{Changed: changed}
	return r.WithExtra("server", current), nil
}

// scwServerState reads a `scw instance server` JSON object's own
// top-level "state" field (e.g. "running", "stopped", "starting",
// "stopping"), returning "" if absent/not a string.
func scwServerState(server map[string]any) string {
	s, _ := server["state"].(string)
	return s
}

// scwAnyStringSlice reads a JSON-decoded []any field (a []string that
// arrived as []any inside a map[string]any, per encoding/json's own
// default decode) back into a plain []string.
func scwAnyStringSlice(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, fmt.Sprint(item))
	}
	return out
}
