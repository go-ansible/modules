package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleValidateArgumentSpec implements (a subset of) Ansible's
// `validate_argument_spec` module: checks a set of provided arguments
// against an argument-spec dictionary shaped like AnsibleModule's own
// `argument_spec` (used inside roles for self-validation).
//
// Args: argument_spec (map[string]any, required) — argument name ->
// {type, required, default, choices}; provided_arguments
// (map[string]any) — the arguments to validate. Real
// validate_argument_spec defaults provided_arguments to the calling
// task's own args when the argument is omitted (via its action
// plugin's access to the task context); this port has no such
// context — a module here only ever sees the args map it was called
// with (module.go's Func signature) — so provided_arguments is treated
// as empty (every non-defaulted arg reported missing) rather than
// magically discovering "the caller's own arguments", a deliberate,
// documented deviation from real Ansible's exact contract.
//
// Per-argument checks: required (fails if absent and no default is
// given); type (a coarse Go-type check — see argMatchesType — for str,
// int, float, bool, list, dict, path, raw; an unrecognized type string
// is not itself an error, matching real Ansible's own leniency toward
// custom/plugin-defined types this port doesn't know about); choices
// (value must equal one of the listed choices, compared via fmt.Sprint
// for a loose string-shaped match rather than real Ansible's
// type-aware equality).
//
// Simplifications vs real validate_argument_spec: no support for
// nested `options` (sub-argument specs for dict/list-of-dict
// arguments), `elements` (per-element type checking within a list),
// `mutually_exclusive`/`required_together`/`required_one_of`/
// `required_if`/`required_by`, or argument aliasing. Every violation
// found is collected and reported together in one Fail message
// (semicolon-joined) rather than stopping at the first one, so a
// caller sees every problem in one run.
func moduleValidateArgumentSpec(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	specRaw, ok := args["argument_spec"]
	if !ok {
		return Result{}, errArg("validate_argument_spec: missing required argument: argument_spec")
	}
	spec, ok := specRaw.(map[string]any)
	if !ok {
		return Result{}, errArg("validate_argument_spec: argument_spec must be a dictionary, got %T", specRaw)
	}

	provided := map[string]any{}
	if v, ok := args["provided_arguments"]; ok {
		if m, ok := v.(map[string]any); ok {
			provided = m
		} else {
			return Result{}, errArg("validate_argument_spec: provided_arguments must be a dictionary, got %T", v)
		}
	}

	violations := validateArguments(spec, provided)
	if len(violations) > 0 {
		return Fail(strings.Join(violations, "; ")), nil
	}
	return Ok("argument spec validation passed"), nil
}

// validateArguments checks provided against spec, returning every
// violation found (empty if none).
func validateArguments(spec, provided map[string]any) []string {
	var violations []string
	for name, rawEntry := range spec {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		required, _ := entry["required"].(bool)
		typ, _ := entry["type"].(string)

		val, present := provided[name]
		if !present {
			if def, ok := entry["default"]; ok {
				val, present = def, true
			}
		}
		if !present {
			if required {
				violations = append(violations, fmt.Sprintf("missing required argument: %s", name))
			}
			continue
		}

		if typ != "" && !argMatchesType(val, typ) {
			violations = append(violations, fmt.Sprintf("argument %s: expected type %s, got %T", name, typ, val))
		}

		if choices, ok := entry["choices"].([]any); ok && len(choices) > 0 {
			if !choiceContains(choices, val) {
				violations = append(violations, fmt.Sprintf("argument %s: value %v is not one of %v", name, val, choices))
			}
		}
	}
	return violations
}

// argMatchesType reports whether val's Go type is compatible with typ
// (an AnsibleModule-style type name). An unrecognized typ always
// matches — this port doesn't fail closed on a type it doesn't know.
func argMatchesType(val any, typ string) bool {
	switch typ {
	case "str", "path":
		_, ok := val.(string)
		return ok
	case "bool":
		_, ok := val.(bool)
		return ok
	case "int":
		switch val.(type) {
		case int, int64, float64:
			return true
		}
		return false
	case "float":
		switch val.(type) {
		case float64, int, int64:
			return true
		}
		return false
	case "list":
		switch val.(type) {
		case []any, []string:
			return true
		}
		return false
	case "dict":
		_, ok := val.(map[string]any)
		return ok
	case "raw":
		return true
	default:
		return true
	}
}

// choiceContains reports whether choices contains a value equal to val
// under a loose fmt.Sprint string comparison.
func choiceContains(choices []any, val any) bool {
	want := fmt.Sprint(val)
	for _, c := range choices {
		if fmt.Sprint(c) == want {
			return true
		}
	}
	return false
}
