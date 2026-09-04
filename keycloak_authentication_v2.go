package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeycloakAuthenticationV2 implements Ansible's
// `keycloak_authentication_v2` (community.general) module: creates,
// updates, or deletes a Keycloak authentication flow, via kcadm.sh
// (see keycloak_common.go's own doc comment for the auth precondition
// and the general kcadm.sh-as-API-client substitution this whole batch
// makes). Unlike `keycloak_authentication` (v1, this batch's own
// keycloak_authentication.go), a flow that already matches the desired
// state is never patched in place: an update goes through the real
// module's own documented "Safe Swap" procedure — read in full from
// keycloak_authentication_v2.py's own module docstring and ported here
// function-for-function against that source (not guessed):
//  1. build a new flow under a temporary alias (alias +
//     temporary_swap_flow_suffix, default "_tmp_for_swap");
//  2. add every desired execution/subflow to it;
//  3. if the OLD flow is bound anywhere (a realm binding — browserFlow/
//     registrationFlow/directGrantFlow/resetCredentialsFlow/
//     clientAuthenticationFlow/dockerAuthenticationFlow/
//     firstBrokerLoginFlow — a client's own
//     authenticationFlowBindingOverrides.browser/direct_grant, or an
//     identity provider's own firstBrokerLoginFlowAlias/
//     postBrokerLoginFlowAlias), every one of those bindings is
//     repointed at the new temporary flow FIRST;
//  4. the old flow is deleted;
//  5. the temporary flow (and every subflow/authenticationConfig alias
//     inside it) is renamed back to the original alias.
//
// A flow that is NOT bound anywhere is instead deleted and recreated
// directly under the original alias (no temporary-name dance needed,
// per keycloak_authentication_v2.py's own is_flow_in_use branch).
//
// Args: realm (required); alias (required); providerId
// (basic-flow|client-flow, default basic-flow); description;
// authenticationExecutions (list of dicts, up to 4 levels of nested
// subflows via each level's own authenticationExecutions — each item
// is EITHER an execution: providerId (required at the top level) +
// requirement (REQUIRED|ALTERNATIVE|DISABLED|CONDITIONAL, required) +
// optional authenticationConfig{alias,config} — OR a subflow: subFlow
// (the new subflow's own alias) + subFlowType (basic-flow|form-flow,
// default basic-flow) + requirement + its own nested
// authenticationExecutions); state (present|absent, default present);
// force_temporary_swap_flow_deletion (bool, default true) — if a
// leftover temporary swap flow from an interrupted prior run is found
// and this is false, the module fails instead of silently deleting it;
// temporary_swap_flow_suffix (default "_tmp_for_swap").
//
// Idempotency: this port builds the same normalized, flattened
// "diff representation" of both the desired executions tree and the
// existing flow's own execution list (recursively stripping
// server-only fields — id, requirementChoices, configurable,
// displayName, description, flowId, and an authenticationConfig's own
// id — from the existing side; adding index/priority/level and an
// authenticationFlow=true marker to the desired side) that
// keycloak_authentication_v2.py's own desired_auth_to_diff_repr/
// existing_auth_to_diff_repr build, and compares them for byte-equal
// JSON — see kcDesiredAuthDiffRepr/kcExistingAuthDiffRepr.
//
// Deviation — providerId validation: real keycloak_authentication_v2.py
// pre-validates every authenticationExecutions[].providerId against
// the realm's own installed authenticator-providers list
// (authentication/authenticator-providers) before doing anything else,
// for a clearer error message. This port does not: an invalid
// providerId surfaces as whatever error kcadm.sh/Keycloak itself
// returns from the execution-creation call, not a dedicated pre-flight
// message — an honest simplification to avoid an extra API round trip
// this port's own architecture does not require for correctness.
func moduleKeycloakAuthenticationV2(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "keycloak_authentication_v2"
	if res, ok := kcadmRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	realm, err := requireString(args, "realm")
	if err != nil {
		return Result{}, err
	}
	alias, err := requireString(args, "alias")
	if err != nil {
		return Result{}, err
	}
	providerID := argString(args, "providerId", "basic-flow")
	if providerID != "basic-flow" && providerID != "client-flow" {
		return Result{}, errArg("%s: providerId must be one of basic-flow, client-flow, got %q", mod, providerID)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	forceSwapDeletion := argBool(args, "force_temporary_swap_flow_deletion", true)
	swapSuffix := argString(args, "temporary_swap_flow_suffix", "_tmp_for_swap")
	var description any
	if d, ok := args["description"]; ok {
		description = d
	}
	execs := argListOfMaps(args, "authenticationExecutions")

	existing, found, err := kcFindFlowByAlias(ctx, conn, realm, alias)
	if err != nil {
		return Result{}, err
	}

	if !found {
		if state == "absent" {
			return Ok(fmt.Sprintf("'%s' is already absent", alias)), nil
		}
		created, fres, err := kcCreateAuthFlow(ctx, conn, realm, mod, alias, providerID, description)
		if err != nil {
			return Result{}, err
		}
		if fres.Failed {
			return fres, nil
		}
		if fres, err := kcCreateExecutionsTree(ctx, conn, realm, mod, created, execs, alias); err != nil {
			return Result{}, err
		} else if fres.Failed {
			return fres, nil
		}
		return Changed(fmt.Sprintf("Authentication flow '%s' with id: '%s' created", alias, kcString(created, "id"))).
			WithExtra("end_state", created), nil
	}

	inUse, err := kcAuthFlowInUse(ctx, conn, realm, existing)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if inUse {
			return Fail(fmt.Sprintf("Flow %s with id %s is in use and therefore cannot be deleted in realm %s",
				kcString(existing, "alias"), kcString(existing, "id"), realm)), nil
		}
		res, err := kcadmDelete(ctx, conn, "authentication/flows/"+kcString(existing, "id"), realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "delete authentication flow "+alias, res), nil
		}
		return Changed(fmt.Sprintf("Authentication flow: %s id: %s is deleted", alias, kcString(existing, "id"))), nil
	}

	// state == present, flow already exists: compare normalized reprs.
	desiredRepr := kcDesiredAuthDiffRepr(alias, providerID, description, execs)
	existingExecs, err := kcGetExecutionsRepresentation(ctx, conn, realm, alias)
	if err != nil {
		return Result{}, err
	}
	existingRepr := kcExistingAuthDiffRepr(existing, existingExecs)
	if kcJSONEqual(desiredRepr, existingRepr) {
		return Ok(fmt.Sprintf("'%s' already matches the desired state", alias)).WithExtra("end_state", existingRepr), nil
	}

	if inUse {
		tmpAlias := alias + swapSuffix
		tmpExisting, tmpFound, err := kcFindFlowByAlias(ctx, conn, realm, tmpAlias)
		if err != nil {
			return Result{}, err
		}
		if tmpFound {
			if !forceSwapDeletion {
				return Fail(fmt.Sprintf("%s: a temporary swap flow '%s' already exists (likely left over from "+
					"an interrupted previous run) and force_temporary_swap_flow_deletion=false; set it to "+
					"true to have this module delete it, or remove it manually first", mod, tmpAlias)), nil
			}
			if err := kcRebindAuthFlowBindings(ctx, conn, realm,
				kcString(tmpExisting, "id"), kcString(tmpExisting, "alias"),
				kcString(existing, "id"), kcString(existing, "alias")); err != nil {
				return Result{}, err
			}
			res, err := kcadmDelete(ctx, conn, "authentication/flows/"+kcString(tmpExisting, "id"), realm)
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return kcadmFailedf(mod, "delete leftover temporary swap flow "+tmpAlias, res), nil
			}
		}

		created, fres, err := kcCreateAuthFlow(ctx, conn, realm, mod, tmpAlias, providerID, description)
		if err != nil {
			return Result{}, err
		}
		if fres.Failed {
			return fres, nil
		}
		if fres, err := kcCreateExecutionsTree(ctx, conn, realm, mod, created, kcAppendSuffixToExecutions(execs, swapSuffix), tmpAlias); err != nil {
			return Result{}, err
		} else if fres.Failed {
			return fres, nil
		}

		if err := kcRebindAuthFlowBindings(ctx, conn, realm,
			kcString(existing, "id"), kcString(existing, "alias"),
			kcString(created, "id"), kcString(created, "alias")); err != nil {
			return Result{}, err
		}
		res, err := kcadmDelete(ctx, conn, "authentication/flows/"+kcString(existing, "id"), realm)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return kcadmFailedf(mod, "delete old authentication flow "+alias, res), nil
		}
		if err := kcRemoveSuffixFromFlowNames(ctx, conn, realm, created, swapSuffix); err != nil {
			return Result{}, err
		}
		finalFlow, _, err := kcFindFlowByAlias(ctx, conn, realm, alias)
		if err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("Authentication flow: %s id: %s updated", alias, kcString(created, "id"))).
			WithExtra("end_state", finalFlow), nil
	}

	// Not bound anywhere: safe to delete and recreate directly.
	res, err := kcadmDelete(ctx, conn, "authentication/flows/"+kcString(existing, "id"), realm)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return kcadmFailedf(mod, "delete authentication flow "+alias, res), nil
	}
	created, fres, err := kcCreateAuthFlow(ctx, conn, realm, mod, alias, providerID, description)
	if err != nil {
		return Result{}, err
	}
	if fres.Failed {
		return fres, nil
	}
	if fres, err := kcCreateExecutionsTree(ctx, conn, realm, mod, created, execs, alias); err != nil {
		return Result{}, err
	} else if fres.Failed {
		return fres, nil
	}
	return Changed(fmt.Sprintf("Authentication flow: %s id: %s updated", alias, kcString(created, "id"))).
		WithExtra("end_state", created), nil
}

// kcFindFlowByAlias lists authentication/flows and returns the one
// whose alias matches, mirroring
// KeycloakAPI.get_authentication_flow_by_alias's own client-side loop
// (the flows collection has no server-side alias filter).
func kcFindFlowByAlias(ctx context.Context, conn remoteexec.Connection, realm, alias string) (map[string]any, bool, error) {
	list, res, err := kcadmListMaps(ctx, conn, "authentication/flows", realm, nil)
	if err != nil {
		return nil, false, err
	}
	if res.RC != 0 {
		return nil, false, fmt.Errorf("listing authentication flows in realm %s: %s", realm, kcadmErrMsg(res))
	}
	if m := kcFindByField(list, "alias", alias); m != nil {
		return m, true, nil
	}
	return nil, false, nil
}

// kcCreateAuthFlow creates an empty top-level flow (POST
// authentication/flows) and re-fetches it by the id kcadm.sh's own -i
// reports, mirroring create_empty_auth_flow.
func kcCreateAuthFlow(ctx context.Context, conn remoteexec.Connection, realm, mod, alias, providerID string, description any) (map[string]any, Result, error) {
	body := map[string]any{"alias": alias, "providerId": providerID, "description": description, "topLevel": true}
	id, res, err := kcadmCreateBodyID(ctx, conn, "authentication/flows", realm, body)
	if err != nil {
		return nil, Result{}, err
	}
	if res.RC != 0 {
		return nil, kcadmFailedf(mod, "create authentication flow "+alias, res), nil
	}
	attrs, present, err := kcadmShow(ctx, conn, "authentication/flows/"+id, realm)
	if err != nil {
		return nil, Result{}, err
	}
	if !present {
		return nil, Fail(fmt.Sprintf("%s: created authentication flow %s (id %s) but could not read it back", mod, alias, id)), nil
	}
	return attrs, Result{}, nil
}

// kcCreateExecutionsTree recursively creates every execution/subflow in
// execs under parentFlowAlias, mirroring create_executions +
// update_execution_requirement_and_config: Keycloak defaults a newly
// created execution's requirement to DISABLED, so every creation is
// followed by a PUT to set the real desired requirement (and, if
// given, its authenticationConfig).
func kcCreateExecutionsTree(ctx context.Context, conn remoteexec.Connection, realm, mod string, topFlow map[string]any, execs []map[string]any, parentFlowAlias string) (Result, error) {
	for _, e := range execs {
		requirement := kcString(e, "requirement")
		providerID := kcString(e, "providerId")
		subFlow := kcString(e, "subFlow")
		subFlowType := kcString(e, "subFlowType")
		if subFlowType == "" {
			subFlowType = "basic-flow"
		}
		var authCfg map[string]any
		if v, ok := e["authenticationConfig"].(map[string]any); ok {
			authCfg = v
		}

		if subFlow != "" {
			body := map[string]any{"alias": subFlow, "provider": "registration-page-form", "type": subFlowType}
			cres, err := kcadmCreateBody(ctx, conn, "authentication/flows/"+parentFlowAlias+"/executions/flow", realm, body)
			if err != nil {
				return Result{}, err
			}
			if cres.RC != 0 {
				return kcadmFailedf(mod, "create subflow "+subFlow, cres), nil
			}
		} else {
			body := map[string]any{"provider": providerID, "requirement": requirement}
			cres, err := kcadmCreateBody(ctx, conn, "authentication/flows/"+parentFlowAlias+"/executions/execution", realm, body)
			if err != nil {
				return Result{}, err
			}
			if cres.RC != 0 {
				return kcadmFailedf(mod, "create execution "+providerID, cres), nil
			}
		}

		curExecs, err := kcGetExecutionsRepresentation(ctx, conn, realm, kcString(topFlow, "alias"))
		if err != nil {
			return Result{}, err
		}
		if len(curExecs) == 0 {
			return Fail(mod + ": no executions found on the flow immediately after creating one"), nil
		}
		created := curExecs[len(curExecs)-1]
		updBody := map[string]any{
			"id":          kcString(created, "id"),
			"providerId":  providerID,
			"requirement": requirement,
			"priority":    created["priority"],
		}
		ures, err := kcadmUpdateBody(ctx, conn, "authentication/flows/"+parentFlowAlias+"/executions", realm, updBody)
		if err != nil {
			return Result{}, err
		}
		if ures.RC != 0 {
			return kcadmFailedf(mod, "set requirement on newly created execution", ures), nil
		}

		if authCfg != nil {
			cres, err := kcadmCreateBody(ctx, conn, "authentication/executions/"+kcString(created, "id")+"/config", realm, authCfg)
			if err != nil {
				return Result{}, err
			}
			if cres.RC != 0 {
				return kcadmFailedf(mod, "set authenticationConfig on newly created execution", cres), nil
			}
		}

		if subFlow != "" {
			children := asMapList(e["authenticationExecutions"])
			if len(children) > 0 {
				if fres, err := kcCreateExecutionsTree(ctx, conn, realm, mod, topFlow, children, subFlow); err != nil {
					return Result{}, err
				} else if fres.Failed {
					return fres, nil
				}
			}
		}
	}
	return Result{}, nil
}

// kcGetExecutionsRepresentation runs `kcadm.sh get
// authentication/flows/<flowAlias>/executions` and, for every entry
// whose authenticationConfig is still a bare config ID (not yet the
// full object), fetches authentication/config/<id> and replaces it —
// mirroring get_executions_representation exactly.
func kcGetExecutionsRepresentation(ctx context.Context, conn remoteexec.Connection, realm, flowAlias string) ([]map[string]any, error) {
	list, res, err := kcadmListMaps(ctx, conn, "authentication/flows/"+flowAlias+"/executions", realm, nil)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("listing executions for authentication flow %s: %s", flowAlias, kcadmErrMsg(res))
	}
	for _, e := range list {
		if cfgID, ok := e["authenticationConfig"].(string); ok && cfgID != "" {
			var cfg map[string]any
			cres, cerr := kcadmGetJSON(ctx, conn, "authentication/config/"+cfgID, realm, nil, &cfg)
			if cerr != nil {
				return nil, cerr
			}
			if cres.RC == 0 {
				e["authenticationConfig"] = cfg
			}
		}
	}
	return list, nil
}

// kcAuthFlowInUse mirrors is_auth_flow_in_use: a flow is "in use" if a
// realm binding points at its alias, a client's own
// authenticationFlowBindingOverrides.browser/direct_grant points at its
// id, or an identity provider's own firstBrokerLoginFlowAlias/
// postBrokerLoginFlowAlias points at its alias.
func kcAuthFlowInUse(ctx context.Context, conn remoteexec.Connection, realm string, flow map[string]any) (bool, error) {
	flowID, flowAlias := kcString(flow, "id"), kcString(flow, "alias")
	realmData, present, err := kcadmShow(ctx, conn, "realms/"+realm, "")
	if err != nil {
		return false, err
	}
	if !present {
		return false, fmt.Errorf("realm %q does not exist", realm)
	}
	for _, k := range kcRealmBindingKeys {
		if kcString(realmData, k) == flowAlias {
			return true, nil
		}
	}
	clients, res, err := kcadmListMaps(ctx, conn, "clients", realm, nil)
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("listing clients in realm %s: %s", realm, kcadmErrMsg(res))
	}
	for _, c := range clients {
		overrides, _ := c["authenticationFlowBindingOverrides"].(map[string]any)
		if kcString(overrides, "browser") == flowID || kcString(overrides, "direct_grant") == flowID {
			return true, nil
		}
	}
	idps, res, err := kcadmListMaps(ctx, conn, "identity-provider/instances", realm, nil)
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("listing identity providers in realm %s: %s", realm, kcadmErrMsg(res))
	}
	for _, idp := range idps {
		if kcString(idp, "firstBrokerLoginFlowAlias") == flowAlias || kcString(idp, "postBrokerLoginFlowAlias") == flowAlias {
			return true, nil
		}
	}
	return false, nil
}

var kcRealmBindingKeys = []string{
	"browserFlow", "registrationFlow", "directGrantFlow", "resetCredentialsFlow",
	"clientAuthenticationFlow", "dockerAuthenticationFlow", "firstBrokerLoginFlow",
}

// kcRebindAuthFlowBindings mirrors rebind_auth_flow_bindings: repoints
// every realm binding, client authenticationFlowBindingOverrides, and
// identity-provider login-flow/post-flow reference from the "from"
// flow onto the "to" flow.
func kcRebindAuthFlowBindings(ctx context.Context, conn remoteexec.Connection, realm, fromID, fromAlias, toID, toAlias string) error {
	realmData, present, err := kcadmShow(ctx, conn, "realms/"+realm, "")
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("realm %q does not exist", realm)
	}
	realmChanged := false
	for _, k := range kcRealmBindingKeys {
		if kcString(realmData, k) == fromAlias {
			realmData[k] = toAlias
			realmChanged = true
		}
	}
	if realmChanged {
		res, err := kcadmUpdateBody(ctx, conn, "realms/"+realm, "", realmData)
		if err != nil {
			return err
		}
		if res.RC != 0 {
			return fmt.Errorf("updating realm %s bindings: %s", realm, kcadmErrMsg(res))
		}
	}

	clients, res, err := kcadmListMaps(ctx, conn, "clients", realm, nil)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("listing clients in realm %s: %s", realm, kcadmErrMsg(res))
	}
	for _, c := range clients {
		overrides, _ := c["authenticationFlowBindingOverrides"].(map[string]any)
		if overrides == nil {
			continue
		}
		changed := false
		if kcString(overrides, "browser") == fromID {
			overrides["browser"] = toID
			changed = true
		}
		if kcString(overrides, "direct_grant") == fromID {
			overrides["direct_grant"] = toID
			changed = true
		}
		if changed {
			c["authenticationFlowBindingOverrides"] = overrides
			res, err := kcadmUpdateBody(ctx, conn, "clients/"+kcString(c, "id"), realm, c)
			if err != nil {
				return err
			}
			if res.RC != 0 {
				return fmt.Errorf("updating client %s flow bindings: %s", kcString(c, "clientId"), kcadmErrMsg(res))
			}
		}
	}

	idps, res, err := kcadmListMaps(ctx, conn, "identity-provider/instances", realm, nil)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("listing identity providers in realm %s: %s", realm, kcadmErrMsg(res))
	}
	for _, idp := range idps {
		changed := false
		if kcString(idp, "firstBrokerLoginFlowAlias") == fromAlias {
			idp["firstBrokerLoginFlowAlias"] = toAlias
			changed = true
		}
		if kcString(idp, "postBrokerLoginFlowAlias") == fromAlias {
			idp["postBrokerLoginFlowAlias"] = toAlias
			changed = true
		}
		if changed {
			res, err := kcadmUpdateBody(ctx, conn, "identity-provider/instances/"+kcString(idp, "alias"), realm, idp)
			if err != nil {
				return err
			}
			if res.RC != 0 {
				return fmt.Errorf("updating identity provider %s flow bindings: %s", kcString(idp, "alias"), kcadmErrMsg(res))
			}
		}
	}
	return nil
}

// kcAppendSuffixToExecutions mirrors append_suffix_to_executions: it
// returns a deep copy of execs with suffix appended to every subflow's
// own alias and every authenticationConfig's own alias, recursively —
// used to give the temporary swap flow's own subflows/configs distinct
// names from the real ones while both coexist.
func kcAppendSuffixToExecutions(execs []map[string]any, suffix string) []map[string]any {
	out := make([]map[string]any, len(execs))
	for i, e := range execs {
		c := make(map[string]any, len(e))
		for k, v := range e {
			c[k] = v
		}
		if cfg, ok := c["authenticationConfig"].(map[string]any); ok && cfg != nil {
			cfgCopy := make(map[string]any, len(cfg))
			for k, v := range cfg {
				cfgCopy[k] = v
			}
			cfgCopy["alias"] = kcString(cfgCopy, "alias") + suffix
			c["authenticationConfig"] = cfgCopy
		}
		if sf := kcString(c, "subFlow"); sf != "" {
			c["subFlow"] = sf + suffix
			if kids := asMapList(c["authenticationExecutions"]); kids != nil {
				c["authenticationExecutions"] = kcAppendSuffixToExecutionsAsAny(kids, suffix)
			}
		}
		out[i] = c
	}
	return out
}

func kcAppendSuffixToExecutionsAsAny(execs []map[string]any, suffix string) []any {
	converted := kcAppendSuffixToExecutions(execs, suffix)
	out := make([]any, len(converted))
	for i, e := range converted {
		out[i] = e
	}
	return out
}

// kcRemoveSuffixFromFlowNames mirrors remove_suffix_from_flow_names:
// renames the top-level flow (dropping suffix), then walks its own
// (now flat) execution list renaming every subflow and stripping the
// suffix from every authenticationConfig's own alias.
func kcRemoveSuffixFromFlowNames(ctx context.Context, conn remoteexec.Connection, realm string, flow map[string]any, suffix string) error {
	newAlias := strings.TrimSuffix(kcString(flow, "alias"), suffix)
	if err := kcRenameAuthFlow(ctx, conn, realm, kcString(flow, "id"), newAlias); err != nil {
		return err
	}
	flow["alias"] = newAlias

	execs, err := kcGetExecutionsRepresentation(ctx, conn, realm, newAlias)
	if err != nil {
		return err
	}
	for _, e := range execs {
		if b, _ := e["authenticationFlow"].(bool); b {
			flowID := kcString(e, "flowId")
			newSubAlias := strings.TrimSuffix(kcString(e, "displayName"), suffix)
			if flowID != "" {
				if err := kcRenameAuthFlow(ctx, conn, realm, flowID, newSubAlias); err != nil {
					return err
				}
			}
		}
		if configurable, _ := e["configurable"].(bool); configurable {
			cfg, ok := e["authenticationConfig"].(map[string]any)
			if !ok || cfg == nil {
				continue
			}
			cfg["alias"] = strings.TrimSuffix(kcString(cfg, "alias"), suffix)
			res, err := kcadmUpdateBody(ctx, conn, "authentication/config/"+kcString(cfg, "id"), realm, cfg)
			if err != nil {
				return err
			}
			if res.RC != 0 {
				return fmt.Errorf("renaming authenticationConfig %s: %s", kcString(cfg, "id"), kcadmErrMsg(res))
			}
		}
	}
	return nil
}

// kcRenameAuthFlow mirrors rename_auth_flow: fetches the flow by id,
// changes its alias, drops the read-only authenticationExecutions key
// (not accepted by the update endpoint), and PUTs it back.
func kcRenameAuthFlow(ctx context.Context, conn remoteexec.Connection, realm, flowID, newAlias string) error {
	attrs, present, err := kcadmShow(ctx, conn, "authentication/flows/"+flowID, realm)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	attrs["alias"] = newAlias
	delete(attrs, "authenticationExecutions")
	res, err := kcadmUpdateBody(ctx, conn, "authentication/flows/"+flowID, realm, attrs)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("renaming authentication flow %s to %s: %s", flowID, newAlias, kcadmErrMsg(res))
	}
	return nil
}

// kcDesiredAuthDiffRepr mirrors desired_auth_to_diff_repr: the
// normalized representation of the DESIRED flow, for comparison
// against kcExistingAuthDiffRepr's own output.
func kcDesiredAuthDiffRepr(alias, providerID string, description any, execs []map[string]any) map[string]any {
	return map[string]any{
		"alias":                    alias,
		"providerId":               providerID,
		"description":              description,
		"topLevel":                 true,
		"authenticationExecutions": kcDesiredExecutionsDiffRepr(execs, 0),
	}
}

// kcDesiredExecutionsDiffRepr mirrors
// desired_executions_to_diff_repr_rec: flattens a (possibly nested)
// desired executions tree into the same flat, level-ordered shape
// Keycloak's own execution list returns.
func kcDesiredExecutionsDiffRepr(execs []map[string]any, level int) []map[string]any {
	var out []map[string]any
	for i, e := range execs {
		norm := map[string]any{
			"requirement": kcString(e, "requirement"),
			"index":       i,
			"priority":    i,
			"level":       level,
		}
		if cfg, ok := e["authenticationConfig"].(map[string]any); ok && cfg != nil {
			norm["authenticationConfig"] = cfg
		}
		if sf := kcString(e, "subFlow"); sf != "" {
			norm["authenticationFlow"] = true
			out = append(out, norm)
			if kids := asMapList(e["authenticationExecutions"]); len(kids) > 0 {
				out = append(out, kcDesiredExecutionsDiffRepr(kids, level+1)...)
			}
		} else {
			norm["providerId"] = kcString(e, "providerId")
			out = append(out, norm)
		}
	}
	return out
}

// kcExistingAuthDiffRepr mirrors existing_auth_to_diff_repr: strips
// server-only fields (id, builtIn at the flow level; id,
// requirementChoices, configurable, displayName, description, flowId
// at the execution level; an authenticationConfig's own id; the
// execution's own redundant top-level alias once its
// authenticationConfig is present) from the existing flow/executions so
// the result compares directly against kcDesiredAuthDiffRepr's output.
func kcExistingAuthDiffRepr(flow map[string]any, execs []map[string]any) map[string]any {
	out := make(map[string]any, len(flow))
	for k, v := range flow {
		out[k] = v
	}
	delete(out, "id")
	delete(out, "builtIn")

	normExecs := make([]map[string]any, 0, len(execs))
	for _, raw := range execs {
		e := make(map[string]any, len(raw))
		for k, v := range raw {
			e[k] = v
		}
		delete(e, "id")
		delete(e, "requirementChoices")
		delete(e, "configurable")
		delete(e, "displayName")
		delete(e, "description")
		delete(e, "flowId")
		if cfg, ok := e["authenticationConfig"].(map[string]any); ok && cfg != nil {
			cfgCopy := make(map[string]any, len(cfg))
			for k, v := range cfg {
				cfgCopy[k] = v
			}
			delete(cfgCopy, "id")
			e["authenticationConfig"] = cfgCopy
			delete(e, "alias")
		}
		normExecs = append(normExecs, e)
	}
	out["authenticationExecutions"] = normExecs
	if _, ok := out["description"]; !ok {
		out["description"] = nil
	}
	return out
}
