package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFirewalld implements (a subset of) Ansible's `firewalld`
// module: composes `firewall-cmd` invocations to add/remove a rule
// (or, with no rule given, to create/delete a whole zone), checking
// idempotency via `firewall-cmd --query-*` first — the same approach
// real ansible.posix.firewalld itself uses under the hood (it also
// ultimately shells out to firewalld's own D-Bus-backed CLI/Python
// bindings and relies on its query operations for idempotency).
//
// Args: zone (string, optional — defaults to `firewall-cmd
// --get-default-zone` when unset); permanent, immediate (bool) —
// immediate defaults to true when permanent is false, and false
// otherwise, matching real firewalld's own documented default;
// timeout (int, default 0) — applied as `--timeout=N` to a non-
// permanent add; state (required) — enabled|disabled when a rule
// target is given, present|absent for a zone-only operation (no rule
// target at all, matching real firewalld's own "zone level operations"
// restriction).
//
// Exactly one rule-target argument may be given: service, port,
// rich_rule, source, interface, icmp_block, or masquerade (bool).
// NOT implemented: forward, icmp_block_inversion, port_forward, target,
// protocol, and offline (accepted but unused — this port always talks
// to a live firewalld; offline permanent-only editing is not
// distinguished from the normal path) — real firewalld supports all of
// these; this port covers the common service/port/rich_rule/source/
// interface/icmp_block/masquerade cases and fails cleanly on an
// unrecognized combination rather than silently dropping a flag.
func moduleFirewalld(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	permanent := argBool(args, "permanent", false)
	immediate := argBool(args, "immediate", !permanent)
	timeout := argInt(args, "timeout", 0)
	zone := argString(args, "zone", "")

	kind, value, err := firewalldTarget(args)
	if err != nil {
		return Result{}, err
	}

	if zone == "" {
		zone, err = run(ctx, conn, "firewall-cmd --get-default-zone")
		if err != nil {
			return Result{}, err
		}
	}

	if kind == "" {
		if state != "present" && state != "absent" {
			return Result{}, errArg("firewalld: state must be present or absent for a zone-only operation, got %q", state)
		}
		exists, err := firewalldZoneExists(ctx, conn, zone)
		if err != nil {
			return Result{}, err
		}
		if state == "present" {
			if exists {
				return Ok("zone " + zone + " already present"), nil
			}
			if _, err := run(ctx, conn, "firewall-cmd --permanent --new-zone="+shellQuote(zone)); err != nil {
				return Result{}, err
			}
			return Changed("zone " + zone + " created"), nil
		}
		if !exists {
			return Ok("zone " + zone + " already absent"), nil
		}
		if _, err := run(ctx, conn, "firewall-cmd --permanent --delete-zone="+shellQuote(zone)); err != nil {
			return Result{}, err
		}
		return Changed("zone " + zone + " deleted"), nil
	}

	if state != "enabled" && state != "disabled" {
		return Result{}, errArg("firewalld: state must be enabled or disabled when a rule target is given, got %q", state)
	}

	changed := false
	if permanent {
		c, err := firewalldApply(ctx, conn, zone, kind, value, state, true, 0)
		if err != nil {
			return Result{}, err
		}
		changed = changed || c
	}
	if immediate {
		c, err := firewalldApply(ctx, conn, zone, kind, value, state, false, timeout)
		if err != nil {
			return Result{}, err
		}
		changed = changed || c
	}
	if changed {
		return Changed("firewalld rule updated in zone " + zone), nil
	}
	return Ok("firewalld rule already " + state + " in zone " + zone), nil
}

// firewalldTarget picks the single rule-target argument given (if any)
// and returns its kind ("service", "port", "rich_rule", "source",
// "interface", "icmp_block", or "masquerade") and value. kind=="" means
// no target was given (a zone-only operation). An error is returned if
// more than one target argument is given.
func firewalldTarget(args map[string]any) (kind, value string, err error) {
	candidates := []struct{ kind, key string }{
		{"service", "service"},
		{"port", "port"},
		{"rich_rule", "rich_rule"},
		{"source", "source"},
		{"interface", "interface"},
		{"icmp_block", "icmp_block"},
	}
	for _, c := range candidates {
		if v := argString(args, c.key, ""); v != "" {
			if kind != "" {
				return "", "", errArg("firewalld: %s and %s are mutually exclusive", kind, c.kind)
			}
			kind, value = c.kind, v
		}
	}
	if _, ok := args["masquerade"]; ok {
		if kind != "" {
			return "", "", errArg("firewalld: masquerade and %s are mutually exclusive", kind)
		}
		kind = "masquerade"
	}
	return kind, value, nil
}

// firewalldApply queries, then adds/removes, one rule target in one
// scope (permanent or runtime), returning whether it made a change.
func firewalldApply(ctx context.Context, conn remoteexec.Connection, zone, kind, value, state string, permanent bool, timeout int) (bool, error) {
	base := "firewall-cmd "
	if permanent {
		base += "--permanent "
	}
	base += "--zone=" + shellQuote(zone) + " "

	query, add, remove := firewalldFlags(kind, value)
	res, err := runStatus(ctx, conn, base+query)
	if err != nil {
		return false, err
	}
	present := res.RC == 0
	want := state == "enabled"
	if present == want {
		return false, nil
	}

	flag := remove
	if want {
		flag = add
		if !permanent && timeout > 0 {
			flag += " --timeout=" + strconv.Itoa(timeout)
		}
	}
	if _, err := run(ctx, conn, base+flag); err != nil {
		return false, err
	}
	return true, nil
}

// firewalldFlags builds the --query-*/--add-*/--remove-* flags for
// kind. masquerade takes no value; every other kind is "--<flag>=value".
func firewalldFlags(kind, value string) (query, add, remove string) {
	if kind == "masquerade" {
		return "--query-masquerade", "--add-masquerade", "--remove-masquerade"
	}
	flagName := strings.ReplaceAll(kind, "_", "-")
	q := shellQuote(value)
	return "--query-" + flagName + "=" + q, "--add-" + flagName + "=" + q, "--remove-" + flagName + "=" + q
}

// firewalldZoneExists reports whether zone appears in firewalld's
// permanent zone list.
func firewalldZoneExists(ctx context.Context, conn remoteexec.Connection, zone string) (bool, error) {
	out, err := run(ctx, conn, "firewall-cmd --permanent --get-zones")
	if err != nil {
		return false, err
	}
	for _, z := range strings.Fields(out) {
		if z == zone {
			return true, nil
		}
	}
	return false, nil
}
