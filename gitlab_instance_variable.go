package modules

import (
	"context"
	"sort"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGitlabInstanceVariable implements Ansible's
// `gitlab_instance_variable` (community.general) module: creates,
// updates, deletes, or purges a GitLab instance's (admin-only) CI/CD
// variables, via `glab api` against GitLab's own GET/POST/PUT/DELETE
// /admin/ci/variables(/:key) — see gitlab_common.go's own doc comment
// for the `glab` substitution and its accepted-but-inert
// api_*/validate_certs/ca_path arguments.
//
// `glab variable list` DOES support an `-i/--instance` flag, but
// `glab variable set`/`delete` do NOT (verified against
// docs.gitlab.com/cli/variable/{set,list,delete}/, per this batch's own
// instruction to check before assuming `glab api` is the only option) —
// there is no dedicated way to create, update, or delete an
// instance-level variable through `glab`'s own subcommand surface at
// all, only to list them. Rather than mix `glab variable list -i` for
// reads with `glab api` for every write, this module uses `glab api`
// uniformly for all four operations — the same choice this batch's
// sibling gitlab_project_variable.go and this module's own sibling
// gitlab_group_variable.go already made for the other two CI/CD-
// variable scopes (see gitlab_group_variable.go's own doc comment for
// why keeping all three consistent matters), and here it is not even
// optional for two of the three operations.
//
// Unlike gitlab_project_variable/gitlab_group_variable, real
// gitlab_instance_variable's own `variables` suboptions have NO
// environment_scope and NO hidden field (verified against its own
// ansible-doc output: description/masked/name/protected/raw/value/
// variable_type only) — GitLab's admin CI variables endpoint has no
// per-environment scoping and no hidden-variable support at all. This
// module reuses gitlab_project_variable.go's own gitlabVarSpec/
// gitlabVarObj struct TYPES (their unused EnvironmentScope/Hidden
// fields simply stay at their zero value here) but has its own
// argument normalizer and request-body builder that never read or send
// either field, rather than reusing gitlabVariableDesiredList/
// gitlabVarBody/gitlabVarScopeQuery, which do.
//
// Args: variables (list of dicts: name+value required for
// state=present, plus description/masked/protected/raw/variable_type
// (default env_var)); purge (bool, default false) — deletes every
// existing variable not named (by name) in variables, state=present
// only; state (present|absent, default present).
//
// A variable is matched to an existing one by name alone (no
// environment_scope to disambiguate, unlike the project/group-scoped
// variants). state=present: no match -> POST (added); a match is
// updated (PUT) only if value/masked/protected/raw/variable_type/
// description differs, else left untouched — real gitlab_instance_
// variable's own doc has no documented hidden-variable non-idempotency
// exception (it has no hidden field at all), so unlike
// gitlab_group_variable this module compares every field unconditionally
// before deciding to update. state=absent: a match is deleted (removed);
// no match is untouched.
//
// Extra["instance_variable"]: {added, updated, removed, untouched} —
// four lists of variable names, matching real gitlab_instance_
// variable's own documented return shape exactly.
func moduleGitlabInstanceVariable(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_instance_variable"); !ok {
		return res, nil
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_instance_variable: state must be one of present, absent, got %q", state)
	}
	purge := argBool(args, "purge", false)
	const base = "admin/ci/variables"

	specs, err := gitlabInstanceVariableDesiredList(args)
	if err != nil {
		return Result{}, err
	}

	var current []gitlabVarObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base+"?per_page=100", nil, true, &current)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_instance_variable: unable to list variables: " + glabErrMsg(lres)), nil
	}
	byName := map[string]gitlabVarObj{}
	for _, v := range current {
		byName[v.Key] = v
	}

	var added, updated, removed, untouched []string
	desired := map[string]bool{}

	for _, s := range specs {
		desired[s.Name] = true
		existing, found := byName[s.Name]

		if state == "absent" {
			if found {
				dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+s.Name, nil, false, nil)
				if err != nil {
					return Result{}, err
				}
				if dres.RC != 0 {
					return Fail("gitlab_instance_variable: unable to delete " + s.Name + ": " + glabErrMsg(dres)), nil
				}
				removed = append(removed, s.Name)
				delete(byName, s.Name)
			} else {
				untouched = append(untouched, s.Name)
			}
			continue
		}

		body := gitlabInstanceVarBody(s)
		if !found {
			cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, nil)
			if err != nil {
				return Result{}, err
			}
			if cres.RC != 0 {
				return Fail("gitlab_instance_variable: unable to create " + s.Name + ": " + glabErrMsg(cres)), nil
			}
			added = append(added, s.Name)
			byName[s.Name] = gitlabVarObj{Key: s.Name, Value: s.Value, Description: s.Description, Masked: s.Masked, Protected: s.Protected, Raw: s.Raw, VariableType: s.VariableType}
			continue
		}

		diff := existing.Value != s.Value ||
			existing.Masked != s.Masked ||
			existing.Protected != s.Protected ||
			existing.Raw != s.Raw ||
			existing.VariableType != s.VariableType ||
			existing.Description != s.Description
		if !diff {
			untouched = append(untouched, s.Name)
			continue
		}
		ures, err := glabAPIJSON(ctx, conn, "PUT", base+"/"+s.Name, body, false, nil)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return Fail("gitlab_instance_variable: unable to update " + s.Name + ": " + glabErrMsg(ures)), nil
		}
		updated = append(updated, s.Name)
	}

	if purge && state == "present" {
		for name, v := range byName {
			if desired[name] {
				continue
			}
			dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+v.Key, nil, false, nil)
			if err != nil {
				return Result{}, err
			}
			if dres.RC != 0 {
				return Fail("gitlab_instance_variable: unable to purge " + v.Key + ": " + glabErrMsg(dres)), nil
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
	return r.WithExtra("instance_variable", summary), nil
}

// gitlabInstanceVarBody builds the POST/PUT request body for one
// instance variable spec — like gitlab_project_variable.go's own
// gitlabVarBody, but omitting environment_scope/hidden, which the admin
// CI variables endpoint does not accept (see this file's own doc
// comment).
func gitlabInstanceVarBody(s gitlabVarSpec) map[string]any {
	return map[string]any{
		"key":           s.Name,
		"value":         s.Value,
		"description":   s.Description,
		"masked":        s.Masked,
		"protected":     s.Protected,
		"raw":           s.Raw,
		"variable_type": s.VariableType,
	}
}

// gitlabInstanceVariableDesiredList normalizes `variables` (a list of
// dicts) into []gitlabVarSpec — like gitlab_project_variable.go's own
// gitlabVariableDesiredList, but for real gitlab_instance_variable's
// own narrower suboption set (no vars dict form, no environment_scope,
// no hidden — see this file's own doc comment).
func gitlabInstanceVariableDesiredList(args map[string]any) ([]gitlabVarSpec, error) {
	var out []gitlabVarSpec
	raw, _ := args["variables"].([]any)
	for _, r := range raw {
		item, ok := r.(map[string]any)
		if !ok {
			return nil, errArg("gitlab_instance_variable: each entry of variables must be a dict")
		}
		name, err := requireString(item, "name")
		if err != nil {
			return nil, errArg("gitlab_instance_variable: %v", err)
		}
		out = append(out, gitlabVarSpec{
			Name:         name,
			Value:        argString(item, "value", ""),
			Description:  argString(item, "description", ""),
			Masked:       argBool(item, "masked", false),
			Protected:    argBool(item, "protected", false),
			Raw:          argBool(item, "raw", false),
			VariableType: argString(item, "variable_type", "env_var"),
		})
	}
	return out, nil
}
