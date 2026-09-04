package modules

import (
	"context"
	"encoding/json"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakUserExecuteActionsEmail implements Ansible's
// `keycloak_user_execute_actions_email` (community.general) module:
// triggers the `execute-actions-email` endpoint for a user, via
// `kcadm.sh update users/<id>/execute-actions-email -r <realm>
// [-q client_id=...] [-q redirect_uri=...] [-q lifespan=...] -f -`,
// piping the JSON array of required-action names as the raw body — see
// keycloak_common.go's own doc comment for the kcadm.sh substitution
// and its `-q`/`-f -` general options. The real endpoint
// (module_utils' own URL_EXECUTE_ACTION, `PUT
// .../users/{id}/execute-actions-email`) takes those three as URL
// query parameters and the action list as the PUT body — this port's
// `-q` flags and `-f -` body match that shape exactly, not a generic
// resource create/update.
//
// Args: id XOR username (one required; username costs one extra
// `kcadm.sh get users -q username=` lookup, matching real
// keycloak_user_execute_actions_email's own documented "supplying only
// username causes an extra lookup call"); actions (default
// [UPDATE_PASSWORD]); client_id; redirect_uri; lifespan; realm
// (default master).
//
// This module ALWAYS reports Changed=true — sending an email is a side
// effect with no idempotent "already sent" state to probe, matching
// real keycloak_user_execute_actions_email's own documented behavior
// exactly.
func moduleKeycloakUserExecuteActionsEmail(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := kcadmRequireBinary(ctx, conn, "keycloak_user_execute_actions_email"); !ok {
		return res, nil
	}
	realm := argString(args, "realm", "master")
	id := argString(args, "id", "")
	username := argString(args, "username", "")
	if id == "" && username == "" {
		return Result{}, errArg("keycloak_user_execute_actions_email: one of id, username is required")
	}
	if id != "" && username != "" {
		return Result{}, errArg("keycloak_user_execute_actions_email: id and username are mutually exclusive")
	}

	if id == "" {
		uid, found, err := kcadmFindUserByUsername(ctx, conn, realm, username)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail("keycloak_user_execute_actions_email: no such user: " + username), nil
		}
		id = uid
	}

	actions := argStringList(args, "actions")
	if len(actions) == 0 {
		actions = []string{"UPDATE_PASSWORD"}
	}
	body, err := json.Marshal(actions)
	if err != nil {
		return Result{}, err
	}

	parts := []string{"update", "users/" + id + "/execute-actions-email", "-r", realm}
	if clientID := argString(args, "client_id", ""); clientID != "" {
		parts = append(parts, "-q", "client_id="+clientID)
	}
	if redirect := argString(args, "redirect_uri", ""); redirect != "" {
		parts = append(parts, "-q", "redirect_uri="+redirect)
	}
	if _, ok := args["lifespan"]; ok {
		parts = append(parts, "-q", "lifespan="+strconv.Itoa(argInt(args, "lifespan", 0)))
	}
	parts = append(parts, "-f", "-")

	res, err := kcadmRunStdin(ctx, conn, string(body), parts...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf("keycloak_user_execute_actions_email", "unable to send execute-actions email to user "+id, res), nil
	}
	r := Changed("Execute actions email sent to user " + id)
	r = r.WithExtra("actions", actions)
	r = r.WithExtra("user_id", id)
	return r, nil
}

// kcadmFindUserByUsername looks up a user's UUID by exact username via
// `kcadm.sh get users -r <realm> -q username=<username>` — matching
// real keycloak_* modules' own exact-username lookup convention (see
// glabResolveUserID's own doc comment for the same shape against
// GitLab). Returns found=false (not an error) if no user has that
// exact username.
func kcadmFindUserByUsername(ctx context.Context, conn remoteexec.Connection, realm, username string) (id string, found bool, err error) {
	var users []map[string]any
	res, err := kcadmGetJSON(ctx, conn, "users", realm, []string{"username=" + username}, &users)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	for _, u := range users {
		if s, _ := u["username"].(string); s == username {
			id, _ := u["id"].(string)
			return id, true, nil
		}
	}
	return "", false, nil
}
