package modules

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulAuthMethod implements Ansible's `consul_auth_method`
// (community.general) module: creates, updates, or deletes a Consul ACL
// auth method via the `consul` CLI's own `consul acl auth-method`
// subcommand — see consul_acl.go's own consulACLRun doc comment for why
// this port substitutes the CLI for real consul_auth_method's
// python-consul/requests HTTP client.
//
// Args: name (required); type (kubernetes|jwt|oidc|aws-iam, required
// when creating — real consul_auth_method documents this field as
// immutable, so this port fails cleanly rather than silently ignoring
// an attempt to change an existing auth method's type); config (dict,
// required when creating) — JSON-encoded and passed as
// `-config=<json>`, matching the `consul acl auth-method create/update`
// CLI's own documented `-config` input methods (inline JSON, `@file`,
// or `-` for stdin — this port always uses inline JSON); description;
// display_name; max_token_ttl; token_locality (local|global); state
// (default present); host/port/scheme/datacenter/ca_path/token (via
// CONSUL_HTTP_TOKEN)/validate_certs.
//
// Auth methods are addressed by name throughout (Consul's ACL API has
// no separate ID for them). Changed compares description/display_name/
// max_token_ttl/token_locality/config against the existing object;
// config is compared structurally (both sides normalized through a
// JSON round-trip so e.g. Go's int and the API's float64 JSON number
// compare equal) rather than as raw text.
func moduleConsulAuthMethod(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("consul_auth_method: state must be present or absent, got %q", state)
	}

	existing, exists, err := consulACLReadMap(ctx, conn, args, "auth-method", "read", []string{"-name", name})
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok("").WithExtra("auth_method", nil), nil
		}
		res, err := consulACLRun(ctx, conn, args, "auth-method", "delete", []string{"-name", name})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("consul_auth_method: unable to delete auth method " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("").WithExtra("auth_method", existing).WithExtra("operation", "delete"), nil
	}

	typ := argString(args, "type", "")
	if exists && typ != "" && typ != consulACLStr(existing, "Type") {
		return Fail("consul_auth_method: type is immutable; auth method " + name + " already has type " + consulACLStr(existing, "Type")), nil
	}
	if !exists && typ == "" {
		return Result{}, errArg("consul_auth_method: type is required when creating auth method %q", name)
	}

	config, _ := args["config"].(map[string]any)
	if !exists && len(config) == 0 {
		return Result{}, errArg("consul_auth_method: config is required when creating auth method %q", name)
	}

	desc := argString(args, "description", consulACLStr(existing, "Description"))
	display := argString(args, "display_name", consulACLStr(existing, "DisplayName"))
	maxTTL := argString(args, "max_token_ttl", consulACLStr(existing, "MaxTokenTTL"))
	locality := argString(args, "token_locality", consulACLStr(existing, "TokenLocality"))

	changed := !exists ||
		desc != consulACLStr(existing, "Description") ||
		display != consulACLStr(existing, "DisplayName") ||
		maxTTL != consulACLStr(existing, "MaxTokenTTL") ||
		locality != consulACLStr(existing, "TokenLocality")
	if _, given := args["config"]; given && !configEqual(config, existing["Config"]) {
		changed = true
	}

	if exists && !changed {
		return Ok("").WithExtra("auth_method", existing), nil
	}

	opts := []string{"-name", name}
	if !exists {
		opts = append(opts, "-type", typ)
	}
	if desc != "" {
		opts = append(opts, "-description", desc)
	}
	if display != "" {
		opts = append(opts, "-display-name", display)
	}
	if maxTTL != "" {
		opts = append(opts, "-max-token-ttl", maxTTL)
	}
	if locality != "" {
		opts = append(opts, "-token-locality", locality)
	}
	if len(config) > 0 {
		b, err := json.Marshal(config)
		if err != nil {
			return Result{}, errArg("consul_auth_method: encoding config: %v", err)
		}
		opts = append(opts, "-config", string(b))
	}

	action := "create"
	if exists {
		action = "update"
	}
	result, exists2, err := consulACLReadMap(ctx, conn, args, "auth-method", action, opts)
	if err != nil {
		return Result{}, err
	}
	if !exists2 {
		return Fail("consul_auth_method: unable to " + action + " auth method " + name), nil
	}
	return Changed("").WithExtra("auth_method", result).WithExtra("operation", action), nil
}

// configEqual compares a desired config map against the existing
// object's own Config field (any — nil, or a map[string]any from JSON),
// normalizing both sides through a JSON round-trip so number/bool
// representations line up regardless of Go-native vs decoded-JSON
// origin.
func configEqual(desired map[string]any, existing any) bool {
	db, _ := json.Marshal(desired)
	eb, _ := json.Marshal(existing)
	var d, e any
	_ = json.Unmarshal(db, &d)
	_ = json.Unmarshal(eb, &e)
	return reflect.DeepEqual(d, e)
}
