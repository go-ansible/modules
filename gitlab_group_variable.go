package modules

import (
	"context"
	"sort"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGitlabGroupVariable implements Ansible's `gitlab_group_variable`
// (community.general) module: creates, updates, deletes, or purges a
// group's CI/CD variables, via `glab api` against GitLab's own
// GET/POST/PUT/DELETE /groups/:id/variables(/:key) — see
// gitlab_common.go's own doc comment for the `glab` substitution and
// its accepted-but-inert api_*/validate_certs/ca_path arguments.
//
// `glab variable set/list/delete` DOES support a `-g/--group` flag for
// group-scoped variables (verified against docs.gitlab.com/cli/
// variable/{set,list,delete}/, per this batch's own instruction to
// check before assuming `glab api` is the only option) — but this
// module uses `glab api` instead, the same choice this batch's sibling
// gitlab_project_variable.go already made for the analogous
// project-scoped resource, kept here for a consistent implementation
// across every CI/CD-variable scope in this codebase (project/group/
// instance). This matters most for gitlab_instance_variable.go, whose
// own resource scope has NO dedicated `glab variable set/delete`
// support at all (only `list -i`, confirmed against the same docs) and
// so must use `glab api` regardless; keeping group_variable on the same
// mechanism avoids three CI/CD-variable-scope modules in one codebase
// behaving subtly differently depending on which one happens to have
// fuller dedicated-command coverage.
//
// This module's arguments (variables, vars, purge, state) are
// identically shaped to real gitlab_project_variable's own — this
// batch's sibling gitlab_project_variable.go already normalizes exactly
// that shape via its own gitlabVariableDesiredList, so this module
// reuses it (and its gitlabVarSpec/gitlabVarObj/gitlabVarScopeQuery/
// gitlabVarBody types/helpers) directly rather than duplicating them;
// the only difference from gitlab_project_variable is the resource
// identifier argument's own name (group, not project) and the API base
// path (groups/:id/variables, not projects/:id/variables). Error
// messages surfaced from gitlabVariableDesiredList itself still read
// "gitlab_project_variable: ..." in the rare malformed-input case (a
// non-dict entry in variables) — a known, harmless cosmetic mismatch
// from reusing a sibling module's own validator rather than a
// functional one.
//
// Args: group (required); variables (list of dicts: name+value required
// for state=present, plus description/environment_scope (default
// "*")/hidden/masked/protected/raw/variable_type (default env_var)) —
// OR vars (a dict of key -> either a plain value or the same attribute
// dict shape); purge (bool, default false) — deletes every existing
// variable not named (by name+environment_scope pair) in
// variables/vars, state=present only; state (present|absent, default
// present).
//
// A variable is matched to an existing one by its (name,
// environment_scope) pair — GitLab's own actual uniqueness key for a
// group variable. state=present: no match -> POST (added). A match with
// hidden=true is ALWAYS updated (PUT), never compared first — matching
// real gitlab_group_variable's own documented non-idempotency for
// hidden variables ("In this case, the module is not idempotent"). A
// match with hidden=false is updated only if
// value/masked/protected/raw/variable_type/description differs, else
// left untouched. state=absent: a match is deleted (removed); no match
// is untouched.
//
// Extra["group_variable"]: {added, updated, removed, untouched} — four
// lists of variable names, matching real gitlab_group_variable's own
// documented return shape exactly.
func moduleGitlabGroupVariable(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_group_variable"); !ok {
		return res, nil
	}
	group, err := requireString(args, "group")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_group_variable: state must be one of present, absent, got %q", state)
	}
	purge := argBool(args, "purge", false)
	base := "groups/" + glabEncodeID(group) + "/variables"

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
		return Fail("gitlab_group_variable: unable to list variables: " + glabErrMsg(lres)), nil
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
					return Fail("gitlab_group_variable: unable to delete " + s.Name + ": " + glabErrMsg(dres)), nil
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
				return Fail("gitlab_group_variable: unable to create " + s.Name + ": " + glabErrMsg(cres)), nil
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
			return Fail("gitlab_group_variable: unable to update " + s.Name + ": " + glabErrMsg(ures)), nil
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
				return Fail("gitlab_group_variable: unable to purge " + v.Key + ": " + glabErrMsg(dres)), nil
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
	return r.WithExtra("group_variable", summary), nil
}
