package modules

import (
	"context"
	"fmt"
	"sort"

	remoteexec "github.com/go-remoteexec/transport"
)

// gitlabVarSpec is one desired CI/CD variable, normalized from either
// the `variables` list or the older `vars` dict — see
// gitlabVariableDesiredList.
type gitlabVarSpec struct {
	Name             string
	Value            string
	Description      string
	EnvironmentScope string
	Hidden           bool
	Masked           bool
	Protected        bool
	Raw              bool
	VariableType     string
}

// gitlabVarObj is one entry of `glab api projects/:id/variables`'s own
// JSON array.
type gitlabVarObj struct {
	Key              string `json:"key"`
	Value            string `json:"value"`
	Description      string `json:"description"`
	EnvironmentScope string `json:"environment_scope"`
	Masked           bool   `json:"masked"`
	Protected        bool   `json:"protected"`
	Raw              bool   `json:"raw"`
	VariableType     string `json:"variable_type"`
}

// moduleGitlabProjectVariable implements Ansible's
// `gitlab_project_variable` (community.general) module: creates,
// updates, deletes, or purges a project's CI/CD variables, via `glab
// api` against GitLab's own GET/POST/PUT/DELETE
// /projects/:id/variables(/:key) — see gitlab_common.go's own doc
// comment for the `glab` substitution and its accepted-but-inert
// api_*/validate_certs/ca_path arguments. `glab` has no dedicated
// project-variable subcommand.
//
// Args: project (required); variables (list of dicts: name+value
// required for state=present, plus description/environment_scope
// (default "*")/hidden/masked/protected/raw/variable_type (default
// env_var)) — OR vars (a dict of key -> either a plain value or a dict
// with the same value/masked/hidden/raw/protected/variable_type/
// environment_scope shape, matching real gitlab_project_variable's own
// documented dual input format; this port normalizes both into the
// same internal list, matching real gitlab_project_variable's own
// documented "This module works internal with this structure, even if
// the older vars parameter is used"); purge (bool, default false) —
// deletes every existing variable not named (by name+environment_scope
// pair) in variables/vars, state=present only; state (present|absent,
// default present).
//
// A variable is matched to an existing one by its (name,
// environment_scope) pair — GitLab's own actual uniqueness key for a
// project variable (a project may hold the same key name scoped to
// different environments) — not by name alone.
//
// state=present: no match -> POST (added). A match with hidden=true is
// ALWAYS updated (PUT), never compared first — matching real
// gitlab_project_variable's own documented non-idempotency for hidden
// variables ("In this case, the module is not idempotent"), since a
// hidden variable's value cannot be read back to compare. A match with
// hidden=false is updated only if value/masked/protected/raw/
// variable_type/description differs, else left untouched. state=absent:
// a match is deleted (removed); no match is untouched.
//
// Extra["project_variable"]: {added, updated, removed, untouched} —
// four lists of variable names, matching real gitlab_project_variable's
// own documented return shape exactly.
func moduleGitlabProjectVariable(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_project_variable"); !ok {
		return res, nil
	}
	project, err := requireString(args, "project")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_project_variable: state must be one of present, absent, got %q", state)
	}
	purge := argBool(args, "purge", false)
	base := "projects/" + glabEncodeID(project) + "/variables"

	specs, err := gitlabVariableDesiredList(args)
	if err != nil {
		return Result{}, err
	}

	var current []gitlabVarObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base+"?per_page=100", nil, true, &current)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_project_variable: unable to list variables: " + glabErrMsg(lres)), nil
	}
	type key struct{ name, scope string }
	byKey := map[key]gitlabVarObj{}
	for _, v := range current {
		byKey[key{v.Key, v.EnvironmentScope}] = v
	}

	var added, updated, removed, untouched []string
	desired := map[key]bool{}

	for _, s := range specs {
		k := key{s.Name, s.EnvironmentScope}
		desired[k] = true
		existing, found := byKey[k]

		if state == "absent" {
			if found {
				dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+s.Name+gitlabVarScopeQuery(s.EnvironmentScope), nil, false, nil)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("gitlab_project_variable: unable to delete " + s.Name + ": " + glabErrMsg(dres)), nil
				}
				removed = append(removed, s.Name)
				delete(byKey, k)
			} else {
				untouched = append(untouched, s.Name)
			}
			continue
		}

		body := gitlabVarBody(s)
		if !found {
			cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, nil)
			if err != nil {
				return Result{}, err
			}
			if cres.RC != 0 {
				return Fail("gitlab_project_variable: unable to create " + s.Name + ": " + glabErrMsg(cres)), nil
			}
			added = append(added, s.Name)
			byKey[k] = gitlabVarObj{Key: s.Name, Value: s.Value, Description: s.Description, EnvironmentScope: s.EnvironmentScope, Masked: s.Masked, Protected: s.Protected, Raw: s.Raw, VariableType: s.VariableType}
			continue
		}

		diff := s.Hidden ||
			existing.Value != s.Value ||
			existing.Masked != s.Masked ||
			existing.Protected != s.Protected ||
			existing.Raw != s.Raw ||
			existing.VariableType != s.VariableType ||
			existing.Description != s.Description
		if !diff {
			untouched = append(untouched, s.Name)
			continue
		}
		ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+s.Name+gitlabVarScopeQuery(s.EnvironmentScope), body, false, nil)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return Fail("gitlab_project_variable: unable to update " + s.Name + ": " + glabErrMsg(ures)), nil
		}
		updated = append(updated, s.Name)
	}

	if purge && state == "present" {
		for k, v := range byKey {
			if desired[k] {
				continue
			}
			dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+v.Key+gitlabVarScopeQuery(v.EnvironmentScope), nil, false, nil)
			if err != nil {
				return Result{}, err
			}
			if dres.RC != 0 {
				return Fail("gitlab_project_variable: unable to purge " + v.Key + ": " + glabErrMsg(dres)), nil
			}
			removed = append(removed, v.Key)
		}
	}

	sort.Strings(added)
	sort.Strings(updated)
	sort.Strings(removed)
	sort.Strings(untouched)
	summary := map[string]any{"added": orEmpty(added), "updated": orEmpty(updated), "removed": orEmpty(removed), "untouched": orEmpty(untouched)}
	changed := len(added) > 0 || len(updated) > 0 || len(removed) > 0
	r := Result{Changed: changed}
	return r.WithExtra("project_variable", summary), nil
}

// gitlabVarScopeQuery renders the `?filter[environment_scope]=`
// GitLab's own single-variable endpoints require to disambiguate which
// scoped copy of a key a GET/PUT/DELETE addresses, omitted for the
// default "*" scope (matching GitLab's own documented default).
func gitlabVarScopeQuery(scope string) string {
	if scope == "" || scope == "*" {
		return ""
	}
	return "?filter[environment_scope]=" + scope
}

// gitlabVarBody builds the POST/PUT request body for one variable spec.
func gitlabVarBody(s gitlabVarSpec) map[string]any {
	return map[string]any{
		"key":               s.Name,
		"value":             s.Value,
		"description":       s.Description,
		"environment_scope": s.EnvironmentScope,
		"hidden":            s.Hidden,
		"masked":            s.Masked,
		"protected":         s.Protected,
		"raw":               s.Raw,
		"variable_type":     s.VariableType,
	}
}

// gitlabVariableDesiredList normalizes `variables` (a list of dicts)
// and `vars` (a dict of key -> plain value or attribute dict) into one
// []gitlabVarSpec — see moduleGitlabProjectVariable's own doc comment.
func gitlabVariableDesiredList(args map[string]any) ([]gitlabVarSpec, error) {
	var out []gitlabVarSpec

	if raw, ok := args["variables"].([]any); ok {
		for _, r := range raw {
			item, ok := r.(map[string]any)
			if !ok {
				return nil, errArg("gitlab_project_variable: each entry of variables must be a dict")
			}
			name, err := requireString(item, "name")
			if err != nil {
				return nil, errArg("gitlab_project_variable: %v", err)
			}
			out = append(out, gitlabVarSpec{
				Name:             name,
				Value:            argString(item, "value", ""),
				Description:      argString(item, "description", ""),
				EnvironmentScope: argString(item, "environment_scope", "*"),
				Hidden:           argBool(item, "hidden", false),
				Masked:           argBool(item, "masked", false),
				Protected:        argBool(item, "protected", false),
				Raw:              argBool(item, "raw", false),
				VariableType:     argString(item, "variable_type", "env_var"),
			})
		}
	}

	if vars, ok := args["vars"].(map[string]any); ok {
		for name, v := range vars {
			spec := gitlabVarSpec{Name: name, EnvironmentScope: "*", VariableType: "env_var"}
			switch val := v.(type) {
			case map[string]any:
				spec.Value = argString(val, "value", "")
				spec.Description = argString(val, "description", "")
				spec.EnvironmentScope = argString(val, "environment_scope", "*")
				spec.Hidden = argBool(val, "hidden", false)
				spec.Masked = argBool(val, "masked", false)
				spec.Protected = argBool(val, "protected", false)
				spec.Raw = argBool(val, "raw", false)
				spec.VariableType = argString(val, "variable_type", "env_var")
			default:
				spec.Value = fmt.Sprint(val)
			}
			out = append(out, spec)
		}
	}

	return out, nil
}
