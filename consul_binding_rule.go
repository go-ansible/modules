package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulBindingRule implements Ansible's `consul_binding_rule`
// (community.general) module: creates, updates, or deletes a Consul ACL
// binding rule via the `consul` CLI's own `consul acl binding-rule`
// subcommand — see consul_acl.go's own consulACLRun doc comment for why
// this port substitutes the CLI for real consul_binding_rule's
// python-consul/requests HTTP client.
//
// Args: name (required) — real consul_binding_rule's own doc is explicit
// that Consul's binding-rule API has no name field at all: "since the
// API does not support a name, it is prefixed to the description" as
// "<name>: <description>" (confirmed against real consul_binding_rule's
// own documented RETURN VALUES example: Description: 'my_name: example
// rule' for name=my_name, description="example rule"). This port
// reproduces that exact encoding (consulBindingRuleCompose) when
// creating/updating a rule and matches it back when looking one up
// (consulBindingRuleFind); auth_method
// (required) — binding rules are addressed by (auth_method, name) since
// `consul acl binding-rule read/update/delete` only take a raw -id, and
// this port has no name-to-ID index other than listing every rule for
// the auth method and matching the composed description
// (consulBindingRuleFind); bind_type (service|node|role|
// templated-policy); bind_name; bind_vars (dict) — see the deviation
// note below; description; selector; state (default present);
// host/port/scheme/datacenter/ca_path/token (via CONSUL_HTTP_TOKEN)/
// validate_certs.
//
// Deviation from real consul_binding_rule: the `consul acl binding-rule
// create`/`update` CLI (per HashiCorp's own command reference) exposes
// no -bind-vars flag for templated-policy variables, so this port cannot
// apply bind_vars through the CLI substitution — a non-empty bind_vars
// argument fails cleanly (Result{Failed:true}) rather than silently
// dropping it, matching this batch's own hard rule to document an
// unreproducible deviation rather than fake it.
func moduleConsulBindingRule(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	authMethod, err := requireString(args, "auth_method")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("consul_binding_rule: state must be present or absent, got %q", state)
	}
	if bv, _ := args["bind_vars"].(map[string]any); len(bv) > 0 {
		return Fail("consul_binding_rule: bind_vars is not supported by this port (the `consul` CLI's own `acl binding-rule` subcommand has no equivalent flag)"), nil
	}

	existing, err := consulBindingRuleFind(ctx, conn, args, authMethod, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if existing == nil {
			return Ok("").WithExtra("binding_rule", nil), nil
		}
		id := consulACLStr(existing, "ID")
		res, err := consulACLRun(ctx, conn, args, "binding-rule", "delete", []string{"-id", id})
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("consul_binding_rule: unable to delete binding rule " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("").WithExtra("binding_rule", existing).WithExtra("operation", "delete"), nil
	}

	description := argString(args, "description", "")
	composed := consulBindingRuleCompose(name, description)
	bindType := argString(args, "bind_type", "")
	bindName := argString(args, "bind_name", "")
	selector := argString(args, "selector", "")

	if existing != nil {
		existingBindType := consulACLStr(existing, "BindType")
		existingBindName := consulACLStr(existing, "BindName")
		existingSelector := consulACLStr(existing, "Selector")
		existingDesc := consulACLStr(existing, "Description")
		if bindType == "" {
			bindType = existingBindType
		}
		if bindName == "" {
			bindName = existingBindName
		}
		unchanged := bindType == existingBindType && bindName == existingBindName &&
			selector == existingSelector && composed == existingDesc
		if unchanged {
			return Ok("").WithExtra("binding_rule", existing), nil
		}
	}

	opts := []string{"-method", authMethod, "-description", composed}
	if bindType != "" {
		opts = append(opts, "-bind-type", bindType)
	}
	if bindName != "" {
		opts = append(opts, "-bind-name", bindName)
	}
	if selector != "" {
		opts = append(opts, "-selector", selector)
	}

	action := "create"
	if existing != nil {
		opts = append([]string{"-id", consulACLStr(existing, "ID")}, opts...)
		action = "update"
	}
	result, exists2, err := consulACLReadMap(ctx, conn, args, "binding-rule", action, opts)
	if err != nil {
		return Result{}, err
	}
	if !exists2 {
		return Fail("consul_binding_rule: unable to " + action + " binding rule " + name), nil
	}
	return Changed("").WithExtra("binding_rule", result).WithExtra("operation", action), nil
}

// consulBindingRuleCompose builds the "<name>: <description>" (or bare
// "<name>" when description is empty) synthetic Description real
// consul_binding_rule itself uses to fake a name field Consul's API
// doesn't have — see this module's own doc comment.
func consulBindingRuleCompose(name, description string) string {
	if description == "" {
		return name
	}
	return name + ": " + description
}

// consulBindingRuleFind lists every binding rule for authMethod (`consul
// acl binding-rule list -format=json`, filtered client-side since this
// port could not confirm the CLI's own list subcommand supports a
// -method filter) and returns the first whose Description matches
// name's own composed form, or nil if none does.
func consulBindingRuleFind(ctx context.Context, conn remoteexec.Connection, args map[string]any, authMethod, name string) (map[string]any, error) {
	list, err := consulACLReadList(ctx, conn, args, "binding-rule", nil)
	if err != nil {
		return nil, err
	}
	prefix := name + ": "
	for _, r := range list {
		if consulACLStr(r, "AuthMethod") != authMethod {
			continue
		}
		desc := consulACLStr(r, "Description")
		if desc == name || strings.HasPrefix(desc, prefix) {
			return r, nil
		}
	}
	return nil, nil
}
