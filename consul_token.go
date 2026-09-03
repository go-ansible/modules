package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulToken implements Ansible's `consul_token`
// (community.general) module: creates, updates, or deletes a Consul ACL
// token via the `consul` CLI's own `consul acl token` subcommand — see
// consul_acl.go's own consulACLRun doc comment for why this port
// substitutes the CLI for real consul_token's python-consul/requests
// HTTP client.
//
// Args: accessor_id — if given, identifies an existing token to
// update/delete (via `consul acl token read -id <accessor_id>`); if
// omitted for state=present, this port always creates a brand NEW token
// (matching real consul_token's own documented behavior: with no
// accessor_id, "a UUID is generated for this field", so there is no
// existing token to look up — the same reasoning consul_session.go's
// own state=present documents for sessions having no stable identity
// either); secret_id (create only — `-secret`); description; policies/
// roles ([]map{id,name}) and service_identities/node_identities (same
// shapes as consul_role.go's own, see its doc comment for the "absent
// key = unchanged, empty list = cleared" rule shared here); local
// (bool) — `-local` on create/update when explicitly true; this port
// cannot un-set an already-local token back to global (no negation flag
// documented for `consul acl token update`), matching consul_role.go's
// own equivalent limitation note for identity flags generally being
// additive-only at the CLI layer; expiration_ttl — `-expires-ttl`,
// applied ONLY at create time, matching real consul_token's own
// documented "Ignored when the token is updated!"; templated_policies —
// unsupported, same deviation as consul_role.go (no CLI flag exists);
// state (default present); host/port/scheme/datacenter/ca_path/token
// (via CONSUL_HTTP_TOKEN)/validate_certs.
//
// Extra["token"]: the token object as read/created/updated.
// Extra["operation"]: "create"/"update"/"delete", set only when Changed.
func moduleConsulToken(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("consul_token: state must be present or absent, got %q", state)
	}
	if len(consulACLMapList(args, "templated_policies")) > 0 {
		return Fail("consul_token: templated_policies is not supported by this port (the `consul` CLI's own `acl token` subcommand has no equivalent flag)"), nil
	}
	accessorID := argString(args, "accessor_id", "")

	var existing map[string]any
	var exists bool
	var err error
	if accessorID != "" {
		existing, exists, err = consulACLReadMap(ctx, conn, args, "token", "read", []string{"-id", accessorID})
		if err != nil {
			return Result{}, err
		}
	}

	if state == "absent" {
		if !exists {
			return Ok("").WithExtra("token", nil), nil
		}
		res, err := consulACLRun(ctx, conn, args, "token", "delete", []string{"-id", accessorID})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("consul_token: unable to delete token " + accessorID + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("").WithExtra("token", existing).WithExtra("operation", "delete"), nil
	}

	desc := argString(args, "description", consulACLStr(existing, "Description"))

	policiesGiven := consulACLMapList(args, "policies")
	_, policiesKeyPresent := args["policies"]
	rolesGiven := consulACLMapList(args, "roles")
	_, rolesKeyPresent := args["roles"]
	serviceGiven := consulACLMapList(args, "service_identities")
	_, serviceKeyPresent := args["service_identities"]
	nodeGiven := consulACLMapList(args, "node_identities")
	_, nodeKeyPresent := args["node_identities"]

	changed := !exists || desc != consulACLStr(existing, "Description")
	if policiesKeyPresent {
		if !consulACLStrSliceEqual(consulACLRefKeys(consulExistingList(existing, "Policies"), "ID", "Name"), consulACLRefKeys(policiesGiven, "id", "name")) {
			changed = true
		}
	}
	if rolesKeyPresent {
		if !consulACLStrSliceEqual(consulACLRefKeys(consulExistingList(existing, "Roles"), "ID", "Name"), consulACLRefKeys(rolesGiven, "id", "name")) {
			changed = true
		}
	}
	if serviceKeyPresent {
		if !consulACLStrSliceEqual(consulServiceIdentityTokens(consulExistingList(existing, "ServiceIdentities")), consulServiceIdentityTokens(consulServiceIdentityArgsToAPI(serviceGiven))) {
			changed = true
		}
	}
	if nodeKeyPresent {
		if !consulACLStrSliceEqual(consulNodeIdentityTokens(consulExistingList(existing, "NodeIdentities")), consulNodeIdentityTokens(consulNodeIdentityArgsToAPI(nodeGiven))) {
			changed = true
		}
	}

	if exists && !changed {
		return Ok("").WithExtra("token", existing), nil
	}

	var opts []string
	if desc != "" {
		opts = append(opts, "-description", desc)
	}
	if policiesKeyPresent {
		opts = append(opts, consulPolicyRefOpts(policiesGiven)...)
	} else if exists {
		opts = append(opts, consulPolicyRefOpts(consulExistingRefArgs(consulExistingList(existing, "Policies")))...)
	}
	if rolesKeyPresent {
		opts = append(opts, consulRoleRefOpts(rolesGiven)...)
	} else if exists {
		opts = append(opts, consulRoleRefOpts(consulExistingRefArgs(consulExistingList(existing, "Roles")))...)
	}
	if serviceKeyPresent {
		opts = append(opts, consulServiceIdentityOpts(serviceGiven)...)
	} else if exists {
		opts = append(opts, consulServiceIdentityOpts(consulServiceIdentityAPIToArgs(consulExistingList(existing, "ServiceIdentities")))...)
	}
	if nodeKeyPresent {
		opts = append(opts, consulNodeIdentityOpts(nodeGiven)...)
	} else if exists {
		opts = append(opts, consulNodeIdentityOpts(consulNodeIdentityAPIToArgs(consulExistingList(existing, "NodeIdentities")))...)
	}
	if argBool(args, "local", false) {
		opts = append(opts, "-local")
	}

	action := "create"
	if exists {
		opts = append([]string{"-id", accessorID}, opts...)
		action = "update"
	} else {
		if accessorID != "" {
			opts = append(opts, "-accessor", accessorID)
		}
		if secret := argString(args, "secret_id", ""); secret != "" {
			opts = append(opts, "-secret", secret)
		}
		if ttl := argString(args, "expiration_ttl", ""); ttl != "" {
			opts = append(opts, "-expires-ttl", ttl)
		}
	}

	result, exists2, err := consulACLReadMap(ctx, conn, args, "token", action, opts)
	if err != nil {
		return Result{}, err
	}
	if !exists2 {
		return Fail("consul_token: unable to " + action + " token"), nil
	}
	return Changed("").WithExtra("token", result).WithExtra("operation", action), nil
}
