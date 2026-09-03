package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulRole implements Ansible's `consul_role` (community.general)
// module: creates, updates, or deletes a Consul ACL role via the
// `consul` CLI's own `consul acl role` subcommand — see consul_acl.go's
// own consulACLRun doc comment for why this port substitutes the CLI for
// real consul_role's python-consul/requests HTTP client.
//
// Args: name (required); description (if omitted, an existing
// description is left unchanged, matching real consul_role); policies
// ([]map{id,name}, one of id/name per entry) — if the `policies` key is
// absent from args, any currently-attached policies are left unchanged;
// an explicit empty list clears them (both matching real consul_role's
// own documented semantics); service_identities ([]map{service_name
// (alias name), datacenters}) and node_identities ([]map{node_name
// (alias name), datacenter}) follow the same "absent key = unchanged,
// empty list = cleared" rule; templated_policies ([]map{template_name,
// template_variables}) — see the deviation note below; state (default
// present: present|absent); host/port/scheme/datacenter/ca_path/token
// (via CONSUL_HTTP_TOKEN)/validate_certs as every other consul_acl_*
// module in this port.
//
// Roles are addressed by name for the initial lookup (`consul acl role
// read -name <name>`), then by the ID that lookup returns for
// update/delete, matching real consul_role's own name-to-ID resolution.
//
// Changed comparison: description is a plain string compare; policies/
// service_identities/node_identities are compared as SETS of a
// coarse fingerprint per entry (see consul_acl.go's own
// consulACLRefKeys/consulServiceIdentityTokens/consulNodeIdentityTokens)
// — a policy given by name only compares equal to itself, but not
// automatically to the same policy given by bare ID elsewhere, since
// this port does not resolve one to the other (see consulACLRefKeys's
// own doc comment for that documented simplification). Any difference
// triggers `consul acl role update -id <ID>` passing the FULL desired
// set for every list field (this port always supplies -policy-id/
// -policy-name/-service-identity/-node-identity flags reflecting the
// merged desired state — reusing the just-read existing values for any
// field omitted from args — since Consul's own `role update` command
// replaces each list field wholesale, not additively).
//
// Deviation from real consul_role: the `consul acl role create`/`update`
// CLI (per HashiCorp's own command reference) exposes no
// -templated-policy flag at all, so this port cannot apply
// templated_policies through the CLI substitution — a non-empty
// templated_policies argument fails cleanly (Result{Failed:true}) rather
// than silently ignoring it.
func moduleConsulRole(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("consul_role: state must be present or absent, got %q", state)
	}
	if len(consulACLMapList(args, "templated_policies")) > 0 {
		return Fail("consul_role: templated_policies is not supported by this port (the `consul` CLI's own `acl role` subcommand has no equivalent flag)"), nil
	}

	existing, exists, err := consulACLReadMap(ctx, conn, args, "role", "read", []string{"-name", name})
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok("").WithExtra("role", nil), nil
		}
		id := consulACLStr(existing, "ID")
		res, err := consulACLRun(ctx, conn, args, "role", "delete", []string{"-id", id})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("consul_role: unable to delete role " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("").WithExtra("role", existing).WithExtra("operation", "delete"), nil
	}

	desc := argString(args, "description", consulACLStr(existing, "Description"))

	policiesGiven := consulACLMapList(args, "policies")
	_, policiesKeyPresent := args["policies"]

	desiredServiceIdentities := consulExistingList(existing, "ServiceIdentities")
	serviceGiven := consulACLMapList(args, "service_identities")
	_, serviceKeyPresent := args["service_identities"]

	desiredNodeIdentities := consulExistingList(existing, "NodeIdentities")
	nodeGiven := consulACLMapList(args, "node_identities")
	_, nodeKeyPresent := args["node_identities"]

	changed := !exists || desc != consulACLStr(existing, "Description")

	if policiesKeyPresent {
		existingKeys := consulACLRefKeys(consulExistingList(existing, "Policies"), "ID", "Name")
		wantKeys := consulACLRefKeys(policiesGiven, "id", "name")
		if !consulACLStrSliceEqual(existingKeys, wantKeys) {
			changed = true
		}
	}
	if serviceKeyPresent {
		wantAPI := consulServiceIdentityArgsToAPI(serviceGiven)
		if !consulACLStrSliceEqual(consulServiceIdentityTokens(desiredServiceIdentities), consulServiceIdentityTokens(wantAPI)) {
			changed = true
		}
	}
	if nodeKeyPresent {
		wantAPI := consulNodeIdentityArgsToAPI(nodeGiven)
		if !consulACLStrSliceEqual(consulNodeIdentityTokens(desiredNodeIdentities), consulNodeIdentityTokens(wantAPI)) {
			changed = true
		}
	}

	if exists && !changed {
		return Ok("").WithExtra("role", existing), nil
	}

	opts := []string{"-name", name}
	if desc != "" {
		opts = append(opts, "-description", desc)
	}
	if policiesKeyPresent {
		opts = append(opts, consulPolicyRefOpts(policiesGiven)...)
	} else {
		opts = append(opts, consulPolicyRefOpts(consulExistingRefArgs(consulExistingList(existing, "Policies")))...)
	}
	if serviceKeyPresent {
		opts = append(opts, consulServiceIdentityOpts(serviceGiven)...)
	} else {
		opts = append(opts, consulServiceIdentityOpts(consulServiceIdentityAPIToArgs(desiredServiceIdentities))...)
	}
	if nodeKeyPresent {
		opts = append(opts, consulNodeIdentityOpts(nodeGiven)...)
	} else {
		opts = append(opts, consulNodeIdentityOpts(consulNodeIdentityAPIToArgs(desiredNodeIdentities))...)
	}

	action := "create"
	if exists {
		opts = append([]string{"-id", consulACLStr(existing, "ID")}, opts...)
		action = "update"
	}
	result, exists2, err := consulACLReadMap(ctx, conn, args, "role", action, opts)
	if err != nil {
		return Result{}, err
	}
	if !exists2 {
		return Fail("consul_role: unable to " + action + " role " + name), nil
	}
	return Changed("").WithExtra("role", result).WithExtra("operation", action), nil
}

// consulExistingRefArgs converts an API-shaped Policies list ([]{ID,Name})
// back to the args shape ([]{id,name}) so it can be re-fed through
// consulPolicyRefOpts when a field was omitted from args and must be
// carried forward unchanged.
func consulExistingRefArgs(entries []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{"id": consulACLStr(e, "ID"), "name": consulACLStr(e, "Name")})
	}
	return out
}

// consulServiceIdentityAPIToArgs converts an API-shaped ServiceIdentities
// list back to the args shape, for carrying an omitted field forward.
func consulServiceIdentityAPIToArgs(entries []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		var dcs []any
		if raw, ok := e["Datacenters"].([]any); ok {
			dcs = raw
		}
		out = append(out, map[string]any{"service_name": consulACLStr(e, "ServiceName"), "datacenters": dcs})
	}
	return out
}

// consulNodeIdentityAPIToArgs converts an API-shaped NodeIdentities list
// back to the args shape, for carrying an omitted field forward.
func consulNodeIdentityAPIToArgs(entries []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{"node_name": consulACLStr(e, "NodeName"), "datacenter": consulACLStr(e, "Datacenter")})
	}
	return out
}
