package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScalewayIP implements Ansible's `scaleway_ip` (community.general)
// module: creates/updates/deletes a Scaleway flexible IP, via `scw
// instance ip create/get/list/attach/detach/update/delete` — see
// scaleway_common.go's own doc comment for why this port substitutes
// the `scw` CLI for real scaleway_ip's own direct REST calls, and for
// the region->zone mapping (scwZone) used below.
//
// Args: state (present|absent, default present); organization/project
// (exactly one required, regardless of state — matches real
// AnsibleModule's own mutually_exclusive+required_one_of, enforced
// before core() runs either way); region (required); id (UUID of an
// existing IP — omitted means "always create a new one", matching real
// present_strategy's own `wished_ip["id"] not in ip_lookup` check,
// which is unconditionally true when id is None); server (attach
// target; OMITTING it means "detach", matching real scaleway_ip's own
// documented "To unattach an IP do not specify this option"); reverse.
//
// Fidelity note — reverse/server are always diffed, even when the
// argument is omitted: real ip_attributes_should_be_changed compares
// target_ip's current value against wished_ip[...], which defaults to
// None when the arg is omitted — so an existing IP's reverse/server
// gets RESET to empty/detached on a run that simply doesn't mention
// them. This is real scaleway_ip's own documented behavior (not this
// port's choice, and not the usual Ansible "omitted means don't touch"
// convention most other modules in this port follow) — verified
// directly against scaleway_ip.py's own source, per this project's
// "read the reference before implementing" rule.
//
// state=absent: id=="" is always Changed=false (an unset id can never
// match a real IP, matching real absent_strategy's own None-never-a-
// key behavior). Otherwise: list, look up by id; not found ->
// Changed=false; found -> `scw instance ip delete ip=<id> zone=<zone>`.
//
// state=present: list, look up by id. Not found -> `scw instance ip
// create organization-id=/project-id=<org-or-project> [server=<server>]
// zone=<zone>`, Changed=true; if reverse is non-empty, a follow-up `scw
// instance ip update ip=<id> reverse=<reverse> zone=<zone>` (create
// does not accept a reverse= argument per `scw`'s own CLI reference —
// verified, not guessed). Found -> compare current server/reverse
// against wished: an attach/migrate uses `scw instance ip attach
// <id> server=<server> zone=<zone>` (verified as its own subcommand,
// not an `ip update` argument — `scw instance ip update`'s own
// documented argument set has no "server" key); a detach uses `scw
// instance ip detach <id> zone=<zone>`; a reverse change uses `scw
// instance ip update ip=<id> reverse=<reverse> zone=<zone>`. Real
// scaleway_ip issues one PATCH for both; this port may issue two `scw`
// calls when both differ — same net Changed result, documented
// deviation in call count.
//
// Extra["ip"]: the created/current/attached IP object, decoded directly
// from `scw`'s own JSON output (see scaleway_common.go's own "Output
// shape" caveat on why this port cannot assert one exact field-name
// schema and reads defensively).
func moduleScalewayIP(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := scwRequireBinary(ctx, conn, "scaleway_ip"); !ok {
		return res, nil
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("scaleway_ip: state must be present or absent, got %q", state)
	}
	region, err := requireString(args, "region")
	if err != nil {
		return Result{}, err
	}
	zone, err := scwZone(region)
	if err != nil {
		return Result{}, err
	}
	org := argString(args, "organization", "")
	project := argString(args, "project", "")
	if (org == "") == (project == "") {
		return Result{}, errArg("scaleway_ip: exactly one of organization or project must be specified")
	}
	id := argString(args, "id", "")
	server := argString(args, "server", "")
	reverse := argString(args, "reverse", "")

	listRes, err := scwRunJSON(ctx, conn, "instance", "ip", "list", "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	if listRes.RC != 0 {
		return Fail("scaleway_ip: failed to list IPs: " + scwErrMsg(listRes)), nil
	}
	var ips []map[string]any
	if derr := scwDecode(listRes.Stdout, &ips); derr != nil {
		return Result{}, derr
	}
	var current map[string]any
	if id != "" {
		for _, ip := range ips {
			if scwIPStr(ip, "id") == id {
				current = ip
				break
			}
		}
	}

	if state == "absent" {
		if current == nil {
			return Ok(""), nil
		}
		delRes, err := scwRun(ctx, conn, "instance", "ip", "delete", "ip="+id, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if delRes.RC != 0 {
			return Fail("scaleway_ip: failed to delete IP " + id + ": " + scwErrMsg(delRes)), nil
		}
		return Changed(""), nil
	}

	if current == nil {
		createArgs := []string{"instance", "ip", "create"}
		if org != "" {
			createArgs = append(createArgs, "organization-id="+org)
		} else {
			createArgs = append(createArgs, "project-id="+project)
		}
		if server != "" {
			createArgs = append(createArgs, "server="+server)
		}
		createArgs = append(createArgs, "zone="+zone)
		createRes, err := scwRunJSON(ctx, conn, createArgs...)
		if err != nil {
			return Result{}, err
		}
		if createRes.RC != 0 {
			return Fail("scaleway_ip: failed to create IP: " + scwErrMsg(createRes)), nil
		}
		var created map[string]any
		if derr := scwDecode(createRes.Stdout, &created); derr != nil {
			return Result{}, derr
		}
		newID := scwIPStr(created, "id")
		if reverse != "" && newID != "" {
			updRes, err := scwRunJSON(ctx, conn, "instance", "ip", "update", "ip="+newID, "reverse="+reverse, "zone="+zone)
			if err != nil {
				return Result{}, err
			}
			if updRes.RC != 0 {
				return Fail("scaleway_ip: created IP but failed to set reverse: " + scwErrMsg(updRes)), nil
			}
			if derr := scwDecode(updRes.Stdout, &created); derr != nil {
				return Result{}, derr
			}
		}
		return Changed("").WithExtra("ip", created), nil
	}

	changed := false
	curServer := scwIPServerID(current)
	if curServer == "" && server != "" {
		attRes, err := scwRunJSON(ctx, conn, "instance", "ip", "attach", id, "server="+server, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if attRes.RC != 0 {
			return Fail("scaleway_ip: failed to attach IP " + id + " to server " + server + ": " + scwErrMsg(attRes)), nil
		}
		changed = true
	} else if curServer != "" && server == "" {
		detRes, err := scwRunJSON(ctx, conn, "instance", "ip", "detach", id, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if detRes.RC != 0 {
			return Fail("scaleway_ip: failed to detach IP " + id + ": " + scwErrMsg(detRes)), nil
		}
		changed = true
	} else if curServer != "" && server != "" && curServer != server {
		attRes, err := scwRunJSON(ctx, conn, "instance", "ip", "attach", id, "server="+server, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if attRes.RC != 0 {
			return Fail("scaleway_ip: failed to migrate IP " + id + " to server " + server + ": " + scwErrMsg(attRes)), nil
		}
		changed = true
	}
	if scwIPStr(current, "reverse") != reverse {
		updRes, err := scwRunJSON(ctx, conn, "instance", "ip", "update", "ip="+id, "reverse="+reverse, "zone="+zone)
		if err != nil {
			return Result{}, err
		}
		if updRes.RC != 0 {
			return Fail("scaleway_ip: failed to update reverse for IP " + id + ": " + scwErrMsg(updRes)), nil
		}
		changed = true
	}

	getRes, err := scwRunJSON(ctx, conn, "instance", "ip", "get", id, "zone="+zone)
	if err != nil {
		return Result{}, err
	}
	final := current
	if getRes.RC == 0 {
		var got map[string]any
		if derr := scwDecode(getRes.Stdout, &got); derr == nil && got != nil {
			final = got
		}
	}
	res := Result{Changed: changed}
	return res.WithExtra("ip", final), nil
}

// scwIPStr reads a string field off a decoded IP JSON object.
func scwIPStr(m map[string]any, key string) string {
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

// scwIPServerID extracts a server identifier off a decoded IP object's
// "server" field — tolerates it being absent/null (no server attached),
// a plain string (the server ID directly), or a nested {"id":...,
// "name":...} object (the community.general sample's own shape) — see
// scaleway_common.go's own "Output shape" caveat on why this port
// cannot assume one exact schema.
func scwIPServerID(m map[string]any) string {
	if m == nil {
		return ""
	}
	v, ok := m["server"]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	case map[string]any:
		return scwIPStr(s, "id")
	}
	return ""
}
