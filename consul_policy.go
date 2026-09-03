package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulPolicy implements Ansible's `consul_policy`
// (community.general) module: creates, updates, or deletes a Consul ACL
// policy via the `consul` CLI's own `consul acl policy` subcommand — see
// consul_acl.go's own consulACLRun doc comment for why this port
// substitutes the CLI for real consul_policy's python-consul/requests
// HTTP client.
//
// Args: name (required); description; rules (the policy's HCL rule
// document); valid_datacenters ([]string); state (default present:
// present|absent); host (default localhost); port (default 8500);
// scheme (default http); datacenter; ca_path; token (via
// CONSUL_HTTP_TOKEN, see consulACLRun); validate_certs (default true).
//
// Policies are addressed by name (Consul's ACL policy name is unique
// cluster-wide, unlike a role or token). state=present: reads the
// existing policy by `consul acl policy read -name <name>`; absent (not
// found) creates it via `consul acl policy create`; present but
// description/rules/valid_datacenters differ from args updates it via
// `consul acl policy update -id <ID>` (the ID from the just-completed
// read, matching real consul_policy's own update-by-ID after a
// name-based lookup); no difference is a no-op. state=absent: deletes it
// by ID if it exists, no-op otherwise.
//
// Extra["policy"]: the policy object as read/created/updated, its
// Consul API field names (Name/Description/Rules/Datacenters/...)
// unchanged, matching real consul_policy's own Extra["policy"].
// Extra["operation"]: "create"/"update"/"delete", set only when Changed
// (matching real consul_policy's own return value, which is `returned:
// changed`).
func moduleConsulPolicy(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("consul_policy: state must be present or absent, got %q", state)
	}

	existing, exists, err := consulACLReadMap(ctx, conn, args, "policy", "read", []string{"-name", name})
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok("").WithExtra("policy", nil), nil
		}
		id := consulACLStr(existing, "ID")
		res, err := consulACLRun(ctx, conn, args, "policy", "delete", []string{"-id", id})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("consul_policy: unable to delete policy " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("").WithExtra("policy", existing).WithExtra("operation", "delete"), nil
	}

	opts := consulPolicyOpts(args, name)
	if !exists {
		created, exists2, err := consulACLReadMap(ctx, conn, args, "policy", "create", opts)
		if err != nil {
			return Result{}, err
		}
		if !exists2 {
			return Fail("consul_policy: unable to create policy " + name), nil
		}
		return Changed("").WithExtra("policy", created).WithExtra("operation", "create"), nil
	}

	if consulPolicyUnchanged(existing, args) {
		return Ok("").WithExtra("policy", existing), nil
	}
	updOpts := append([]string{"-id", consulACLStr(existing, "ID")}, opts...)
	updated, exists2, err := consulACLReadMap(ctx, conn, args, "policy", "update", updOpts)
	if err != nil {
		return Result{}, err
	}
	if !exists2 {
		return Fail("consul_policy: unable to update policy " + name), nil
	}
	return Changed("").WithExtra("policy", updated).WithExtra("operation", "update"), nil
}

// consulPolicyOpts builds the `-name`/`-description`/`-rules`/
// `-valid-datacenter` (repeated) flags shared by `consul acl policy
// create` and `consul acl policy update`.
func consulPolicyOpts(args map[string]any, name string) []string {
	opts := []string{"-name", name}
	if d := argString(args, "description", ""); d != "" {
		opts = append(opts, "-description", d)
	}
	if r := argString(args, "rules", ""); r != "" {
		opts = append(opts, "-rules", r)
	}
	for _, dc := range argStringList(args, "valid_datacenters") {
		opts = append(opts, "-valid-datacenter", dc)
	}
	return opts
}

// consulPolicyUnchanged reports whether the existing policy (as read
// from `consul acl policy read`) already matches args's own
// description/rules/valid_datacenters — the fields real consul_policy
// itself manages.
func consulPolicyUnchanged(existing map[string]any, args map[string]any) bool {
	if consulACLStr(existing, "Description") != argString(args, "description", "") {
		return false
	}
	if consulACLStr(existing, "Rules") != argString(args, "rules", "") {
		return false
	}
	want := argStringList(args, "valid_datacenters")
	var got []string
	if dcs, ok := existing["Datacenters"].([]any); ok {
		for _, d := range dcs {
			got = append(got, argString(map[string]any{"v": d}, "v", ""))
		}
	}
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] != got[i] {
			return false
		}
	}
	return true
}
