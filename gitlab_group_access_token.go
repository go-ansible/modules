package modules

import (
	"context"
	"sort"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGitlabGroupAccessToken implements Ansible's
// `gitlab_group_access_token` (community.general) module: creates or
// revokes a GitLab group access token, via `glab api` against GitLab's
// own GET/POST/DELETE /groups/:id/access_tokens(/:id) — see
// gitlab_common.go's own doc comment for the `glab` substitution and
// its accepted-but-inert api_*/validate_certs/ca_path arguments. `glab`
// DOES have a `glab token create/list/revoke --group` subcommand family
// for this exact resource (verified against docs.gitlab.com/cli/
// token/*), but this module uses `glab api` instead — the same choice
// this batch's sibling gitlab_project_access_token.go already made for
// the analogous project-scoped resource, kept here for a consistent
// implementation across both access-token scopes in this codebase
// rather than introducing a second, differently-shaped mechanism for
// what is otherwise an identical reconciliation. It reuses that
// sibling's own gitlabAccessTokenObj type and stringSetEqual helper
// (osx_defaults.go) rather than redefining either.
//
// Args: group (required); name (required) — the token's name, this
// port's (and real gitlab_group_access_token's own, per its NOTES)
// matching key, since access tokens have no other natural key; scopes
// (aliased scope, list, required for state=present); access_level
// (default maintainer); expires_at (YYYY-MM-DD, required); recreate
// (never|always|state_change, default never) — controls whether an
// existing token with the same name is revoked and recreated, since a
// token's own scopes/access_level/expiry cannot be changed in place
// (matching real gitlab_group_access_token's own documented NOTES
// exactly: "Access tokens can not be changed... has to be recreated");
// state (present|absent, default present).
//
// state=present: recreate=never leaves an existing same-name token
// alone (Changed=false, no token value in Extra, matching real
// gitlab_group_access_token's own "Token string is contained in the
// result only when access token is created or recreated" NOTE);
// recreate=always always revokes-then-recreates; recreate=state_change
// revokes-then-recreates only if scopes/access_level/expires_at differ
// from the existing token's own. No existing token: always created.
// state=absent: an existing same-name token is revoked (DELETE);
// Changed=false if none matched.
//
// Extra["access_token"]: the created/recreated token object (including
// its one-time-visible Token field) — present only when this run
// actually created or recreated one, matching real
// gitlab_group_access_token's own documented return.
func moduleGitlabGroupAccessToken(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := glabRequireBinary(ctx, conn, "gitlab_group_access_token"); !ok {
		return res, nil
	}

	group, err := requireString(args, "group")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("gitlab_group_access_token: state must be one of present, absent, got %q", state)
	}
	base := "groups/" + glabEncodeID(group) + "/access_tokens"

	var tokens []gitlabAccessTokenObj
	lres, err := glabAPIJSON(ctx, conn, "GET", base+"?per_page=100", nil, true, &tokens)
	if err != nil {
		return Result{}, err
	}
	if lres.RC != 0 {
		return Fail("gitlab_group_access_token: unable to list access tokens: " + glabErrMsg(lres)), nil
	}
	var existing *gitlabAccessTokenObj
	for i := range tokens {
		if tokens[i].Name == name && !tokens[i].Revoked {
			existing = &tokens[i]
			break
		}
	}

	if state == "absent" {
		if existing == nil {
			return Ok(name + " already absent"), nil
		}
		dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(existing.ID), nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_group_access_token: unable to revoke " + name + ": " + glabErrMsg(dres)), nil
		}
		return Changed(name + " revoked"), nil
	}

	scopes := argStringList(args, "scopes")
	if len(scopes) == 0 {
		scopes = argStringList(args, "scope")
	}
	accessLevelName := argString(args, "access_level", "maintainer")
	accessLevel, err := glabAccessLevel(accessLevelName)
	if err != nil {
		return Result{}, errArg("gitlab_group_access_token: %v", err)
	}
	expiresAt, err := requireString(args, "expires_at")
	if err != nil {
		return Result{}, err
	}
	recreate := argString(args, "recreate", "never")

	needCreate := existing == nil
	if existing != nil {
		switch recreate {
		case "always":
			needCreate = true
		case "state_change":
			sc := append([]string{}, scopes...)
			sort.Strings(sc)
			es := append([]string{}, existing.Scopes...)
			sort.Strings(es)
			if !stringSetEqual(sc, es) || existing.AccessLevel != accessLevel || existing.ExpiresAt != expiresAt {
				needCreate = true
			}
		case "never":
			// leave as-is
		default:
			return Result{}, errArg("gitlab_group_access_token: recreate must be one of never, always, state_change, got %q", recreate)
		}
	}

	if !needCreate {
		return Ok(name + " already up to date"), nil
	}

	if existing != nil {
		dres, err := glabAPIJSON(ctx, conn, "DELETE", base+"/"+strconv.Itoa(existing.ID), nil, false, nil)
		if err != nil {
			return Result{}, err
		}
		if dres.RC != 0 {
			return Fail("gitlab_group_access_token: unable to revoke previous " + name + ": " + glabErrMsg(dres)), nil
		}
	}

	body := map[string]any{
		"name":         name,
		"scopes":       scopes,
		"access_level": accessLevel,
		"expires_at":   expiresAt,
	}
	var created gitlabAccessTokenObj
	cres, err := glabAPIJSON(ctx, conn, "POST", base, body, false, &created)
	if err != nil {
		return Result{}, err
	}
	if cres.RC != 0 {
		return Fail("gitlab_group_access_token: unable to create " + name + ": " + glabErrMsg(cres)), nil
	}
	r := Changed(name + " created")
	return r.WithExtra("access_token", created), nil
}
