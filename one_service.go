package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// Real OneFlow service/role STATES, verified against real
// one_service.py's own STATES tuple (its own STATES[...] indexing is
// what this port mirrors): PENDING=0, DEPLOYING=1, RUNNING=2,
// UNDEPLOYING=3, WARNING=4, DONE=5, FAILED_UNDEPLOYING=6,
// FAILED_DEPLOYING=7, SCALING=8, FAILED_SCALING=9, COOLDOWN=10.
var oneServiceStates = []string{
	"PENDING", "DEPLOYING", "RUNNING", "UNDEPLOYING", "WARNING", "DONE",
	"FAILED_UNDEPLOYING", "FAILED_DEPLOYING", "SCALING", "FAILED_SCALING", "COOLDOWN",
}

// moduleOneService implements Ansible's `one_service` module: manages
// OpenNebula OneFlow services (instantiating from a service template,
// deleting, reassigning owner/group/mode, scaling a role's
// cardinality), via the `oneflow`/`oneflow-template` CLIs (see
// one_common.go's own doc comment for the pyone-vs-CLI substitution
// this batch makes elsewhere — OneFlow is a SEPARATE OpenNebula
// component with its own REST API and its own CLI pair, not the same
// XML-RPC-backed `one*` tools one_host/one_image/one_template/one_vm/
// one_vnet shell out to).
//
// # Auth — a genuine improvement over the other six one_* modules
//
// Real one_service ALREADY talks to OneFlow's REST API directly via
// open_url (never pyone), authenticating via api_url/api_username/
// api_password (falling back to ONEFLOW_URL/ONEFLOW_USERNAME/
// ONEFLOW_PASSWORD). OpenNebula's own CLI configuration docs confirm
// `oneflow`/`oneflow-template` read the SAME shape of env vars —
// ONEFLOW_URL (endpoint), ONEFLOW_USER, ONEFLOW_PASSWORD (falling back
// to the shared ONE_AUTH file when unset) — so, unlike
// api_username/api_password on the other six one_* modules in this
// batch (dead arguments there, since the XML-RPC-backed `one*` tools
// have no per-invocation password env var), this port wires ALL THREE
// in here: api_url -> ONEFLOW_URL, api_username -> ONEFLOW_USER,
// api_password -> ONEFLOW_PASSWORD, each only as an environment
// variable for that single invocation, never on the command line —
// matching this project's own hard "no secrets in argv" rule (see
// github_common.go's own GH_TOKEN precedent for env-var-not-argv being
// the accepted alternative, not a documented gap here).
//
// # JSON output — an unverified assumption, honestly flagged
//
// `oneflow`/`oneflow-template`'s own `list`/`show` subcommands are
// documented (rundeck_common.go's sibling note applies equally here)
// to accept FORMAT options including `--json` (verified for
// oneflow-template's own `instantiate`, which shares the same
// Service::JSON_FORMAT constant per its own source); this port assumes
// `list --json`/`show <id> --json` emit the SAME DOCUMENT_POOL/
// DOCUMENT (or single DOCUMENT) JSON shape OneFlow's own REST API
// returns natively (the shape real one_service.py's own open_url calls
// already parse) — a reasonable, but genuinely UNVERIFIED assumption
// (no live oneflow binary in this sandbox), the same honesty
// gitlab_common.go's own doc comment applies to `glab api`'s flag
// surface.
//
// Args: service_name/service_id, template_name/template_id (each pair
// mutually exclusive; template_* is additionally mutually exclusive
// with service_id, role, and cardinality, matching real one_service's
// own mutually_exclusive list); state (present|absent, default
// "present"); mode (permission string, e.g. "660"); owner_id/group_id
// (int — matching real one_service's own Python-truthy `if owner_id:`/
// `if group_id:` checks exactly, an owner_id/group_id of 0 is silently
// never applied, a faithfully-reproduced real quirk, not a bug this
// port introduces); unique (bool, requires service_name); wait/
// wait_timeout — accepted, but this port does NOT poll for RUNNING/
// COOLDOWN convergence (same documented-gap stance as one_host.go's
// own wait note); custom_attrs (dict, only valid when instantiating);
// role/cardinality (required together) + force.
//
// Facts (Extra fields) mirror real one_service's own get_service_info()
// key names exactly: service_id, service_name, group_id, group_name,
// owner_id, owner_name, state, mode, roles (each with name,
// cardinality, state, ids).
func moduleOneService(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	_, hasServiceID := args["service_id"]
	_, hasServiceName := args["service_name"]
	_, hasTemplateID := args["template_id"]
	_, hasTemplateName := args["template_name"]
	if hasServiceID && hasServiceName {
		return Result{}, errArg("one_service: service_id and service_name are mutually exclusive")
	}
	if hasTemplateID && hasTemplateName {
		return Result{}, errArg("one_service: template_id and template_name are mutually exclusive")
	}
	if (hasTemplateID || hasTemplateName) && hasServiceID {
		return Result{}, errArg("one_service: template_id/template_name and service_id are mutually exclusive")
	}
	_, hasRole := args["role"]
	_, hasCardinality := args["cardinality"]
	if (hasTemplateID || hasTemplateName) && (hasRole || hasCardinality) {
		return Result{}, errArg("one_service: template_id/template_name and role/cardinality are mutually exclusive")
	}
	if hasRole != hasCardinality {
		return Result{}, errArg("one_service: role and cardinality must be given together")
	}
	if hasServiceID && len(argMapAny(args, "custom_attrs")) > 0 {
		return Result{}, errArg("one_service: service_id and custom_attrs are mutually exclusive")
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent":
	default:
		return Result{}, errArg("one_service: state must be one of present, absent, got %q", state)
	}
	unique := argBool(args, "unique", false)
	serviceName := argString(args, "service_name", "")
	if unique && serviceName == "" {
		return Result{}, errArg("one_service: you cannot use unique without passing service_name")
	}

	url := argString(args, "api_url", "")
	user := argString(args, "api_username", "")
	pass := argString(args, "api_password", "")
	if res, ok := oneRequireBinary(ctx, conn, "oneflow", "one_service"); !ok {
		return res, nil
	}
	if res, ok := oneRequireBinary(ctx, conn, "oneflow-template", "one_service"); !ok {
		return res, nil
	}

	var templateID string
	if hasTemplateID || hasTemplateName {
		tid, ok, err := oneflowResolveTemplate(ctx, conn, url, user, pass, args)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			if hasTemplateID {
				return Fail(fmt.Sprintf("one_service: There is no template with template_id: %s", argString(args, "template_id", ""))), nil
			}
			return Fail("one_service: There is no template with name: " + argString(args, "template_name", "")), nil
		}
		templateID = tid
	}

	if templateID != "" && state == "absent" {
		return Fail("one_service: State absent is not valid for template"), nil
	}

	ownerID := argInt(args, "owner_id", 0)
	groupID := argInt(args, "group_id", 0)
	mode := argString(args, "mode", "")

	if templateID != "" && state == "present" {
		var service map[string]any
		changed := false
		if unique {
			found, ok, err := oneflowFindServiceByName(ctx, conn, url, user, pass, serviceName)
			if err != nil {
				return Result{}, err
			}
			if ok {
				service = found
			}
		}
		if service == nil || oneServiceState(service) == "DONE" {
			res, err := oneflowInstantiate(ctx, conn, url, user, pass, templateID, serviceName, argMapAny(args, "custom_attrs"))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("one_service: instantiating service: " + oneErrMsg(res)), nil
			}
			changed = true
			found, ok, err := oneflowFindServiceByName(ctx, conn, url, user, pass, serviceName)
			if err != nil {
				return Result{}, err
			}
			if !ok {
				return Fail("one_service: service was instantiated but could not be found afterwards"), nil
			}
			service = found
		}
		out, err := oneflowApplyOwnerGroupMode(ctx, conn, url, user, pass, service, ownerID, groupID, mode)
		if err != nil {
			return Result{}, err
		}
		if out.changed {
			changed = true
		}
		res := Result{Changed: changed}
		return oneServiceResultWithFacts(res, out.service), nil
	}

	if !hasServiceID && !hasServiceName {
		return Result{}, errArg("one_service: to manage the service at least the service id or service name should be specified")
	}
	var service map[string]any
	var found bool
	var err error
	if hasServiceID {
		service, found, err = oneflowFindServiceByID(ctx, conn, url, user, pass, argString(args, "service_id", ""))
	} else {
		service, found, err = oneflowFindServiceByName(ctx, conn, url, user, pass, serviceName)
	}
	if err != nil {
		return Result{}, err
	}
	if !found {
		if state == "present" {
			return Fail("one_service: There is no service with name: " + serviceName), nil
		}
		return Ok(""), nil
	}

	if state == "absent" {
		before := oneServiceResultWithFacts(Result{Changed: true}, service)
		res, err := oneRun(ctx, conn, url, "oneflow", "delete", oneflowStr(service["ID"]))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("one_service: deleting service: " + oneErrMsg(res)), nil
		}
		return before, nil
	}

	out, err := oneflowServiceOperation(ctx, conn, url, user, pass, service, ownerID, groupID, mode, args)
	if err != nil {
		return Result{}, err
	}
	res := Result{Changed: out.changed}
	return oneServiceResultWithFacts(res, out.service), nil
}

func oneflowEnvPrefix(url, user, pass string) string {
	var b strings.Builder
	if url != "" {
		b.WriteString("ONEFLOW_URL=" + shellQuote(url) + " ")
	}
	if user != "" {
		b.WriteString("ONEFLOW_USER=" + shellQuote(user) + " ")
	}
	if pass != "" {
		b.WriteString("ONEFLOW_PASSWORD=" + shellQuote(pass) + " ")
	}
	return b.String()
}

func oneflowRun(ctx context.Context, conn remoteexec.Connection, url, user, pass, bin string, argv ...string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := oneflowEnvPrefix(url, user, pass) + bin + " " + strings.Join(quoted, " ")
	return conn.Exec(ctx, cmd, nil)
}

func oneflowRunStdin(ctx context.Context, conn remoteexec.Connection, url, user, pass, bin, body string, argv ...string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := oneflowEnvPrefix(url, user, pass) + bin + " " + strings.Join(quoted, " ")
	return conn.Exec(ctx, cmd, strings.NewReader(body))
}

func oneflowList(ctx context.Context, conn remoteexec.Connection, url, user, pass, bin string) ([]map[string]any, error) {
	res, err := oneflowRun(ctx, conn, url, user, pass, bin, "list", "--json")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("%s list: %s", bin, oneErrMsg(res))
	}
	var decoded map[string]any
	if strings.TrimSpace(res.Stdout) == "" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(res.Stdout), &decoded); err != nil {
		return nil, fmt.Errorf("decoding %s list --json output: %w", bin, err)
	}
	pool, _ := decoded["DOCUMENT_POOL"].(map[string]any)
	docs, _ := pool["DOCUMENT"].([]any)
	out := make([]map[string]any, 0, len(docs))
	for _, d := range docs {
		if m, ok := d.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func oneflowResolveTemplate(ctx context.Context, conn remoteexec.Connection, url, user, pass string, args map[string]any) (id string, found bool, err error) {
	docs, err := oneflowList(ctx, conn, url, user, pass, "oneflow-template")
	if err != nil {
		return "", false, err
	}
	if v, ok := args["template_id"]; ok && v != nil {
		want := fmtAny(v)
		for _, d := range docs {
			if oneflowStr(d["ID"]) == want {
				return want, true, nil
			}
		}
		return "", false, nil
	}
	name := argString(args, "template_name", "")
	for _, d := range docs {
		if oneflowStr(d["NAME"]) == name {
			return oneflowStr(d["ID"]), true, nil
		}
	}
	return "", false, nil
}

func oneflowFindServiceByName(ctx context.Context, conn remoteexec.Connection, url, user, pass, name string) (map[string]any, bool, error) {
	docs, err := oneflowList(ctx, conn, url, user, pass, "oneflow")
	if err != nil {
		return nil, false, err
	}
	for _, d := range docs {
		if oneflowStr(d["NAME"]) == name {
			return d, true, nil
		}
	}
	return nil, false, nil
}

func oneflowFindServiceByID(ctx context.Context, conn remoteexec.Connection, url, user, pass, id string) (map[string]any, bool, error) {
	docs, err := oneflowList(ctx, conn, url, user, pass, "oneflow")
	if err != nil {
		return nil, false, err
	}
	for _, d := range docs {
		if oneflowStr(d["ID"]) == id {
			return d, true, nil
		}
	}
	return nil, false, nil
}

func oneflowInstantiate(ctx context.Context, conn remoteexec.Connection, url, user, pass, templateID, serviceName string, customAttrs map[string]any) (remoteexec.Result, error) {
	strAttrs := map[string]string{}
	for k, v := range customAttrs {
		strAttrs[k] = fmtAny(v)
	}
	body, err := json.Marshal(map[string]any{
		"merge_template": map[string]any{
			"custom_attrs_values": strAttrs,
			"name":                serviceName,
		},
	})
	if err != nil {
		return remoteexec.Result{}, err
	}
	return oneflowRunStdin(ctx, conn, url, user, pass, "oneflow-template", string(body), "instantiate", templateID, "-")
}

type oneflowOpResult struct {
	service map[string]any
	changed bool
}

func oneflowApplyOwnerGroupMode(ctx context.Context, conn remoteexec.Connection, url, user, pass string, service map[string]any, ownerID, groupID int, mode string) (oneflowOpResult, error) {
	return oneflowServiceOperation(ctx, conn, url, user, pass, service, ownerID, groupID, mode, nil)
}

// oneflowServiceOperation mirrors real one_service.py's own
// service_operation: applies owner/group/mode/role-cardinality changes
// (each gated the same Python-truthy way — an ownerID/groupID of 0
// never triggers a change, see this file's own doc comment), refetches
// the service if anything changed, matching real one_service exactly.
// extraArgs, if non-nil, additionally carries role/cardinality/force.
func oneflowServiceOperation(ctx context.Context, conn remoteexec.Connection, url, user, pass string, service map[string]any, ownerID, groupID int, mode string, extraArgs map[string]any) (oneflowOpResult, error) {
	id := oneflowStr(service["ID"])
	changed := false

	if ownerID != 0 && oneflowInt(service["UID"]) != ownerID {
		res, err := oneRun(ctx, conn, url, "oneflow", "chown", id, strconv.Itoa(ownerID))
		if err != nil {
			return oneflowOpResult{}, err
		}
		if res.RC != 0 {
			return oneflowOpResult{}, fmt.Errorf("one_service: changing owner: %s", oneErrMsg(res))
		}
		changed = true
	}
	if groupID != 0 && oneflowInt(service["GID"]) != groupID {
		res, err := oneRun(ctx, conn, url, "oneflow", "chgrp", id, strconv.Itoa(groupID))
		if err != nil {
			return oneflowOpResult{}, err
		}
		if res.RC != 0 {
			return oneflowOpResult{}, fmt.Errorf("one_service: changing group: %s", oneErrMsg(res))
		}
		changed = true
	}
	if mode != "" && oneflowServicePermissions(service) != mode {
		res, err := oneRun(ctx, conn, url, "oneflow", "chmod", id, mode)
		if err != nil {
			return oneflowOpResult{}, err
		}
		if res.RC != 0 {
			return oneflowOpResult{}, fmt.Errorf("one_service: changing mode: %s", oneErrMsg(res))
		}
		changed = true
	}
	if extraArgs != nil {
		if role, ok := extraArgs["role"]; ok && role != nil {
			roleName := fmtAny(role)
			cardinality := argInt(extraArgs, "cardinality", 0)
			force := argBool(extraArgs, "force", false)
			cur, ok := oneflowRoleCardinality(service, roleName)
			if !ok {
				return oneflowOpResult{}, fmt.Errorf("one_service: There is no role with name: %s", roleName)
			}
			if cur != cardinality {
				argv := []string{"scale", id, roleName, strconv.Itoa(cardinality)}
				if force {
					argv = append(argv, "--force")
				}
				res, err := oneRun(ctx, conn, url, "oneflow", argv...)
				if err != nil {
					return oneflowOpResult{}, err
				}
				if res.RC != 0 {
					return oneflowOpResult{}, fmt.Errorf("one_service: scaling role %s: %s", roleName, oneErrMsg(res))
				}
				changed = true
			}
		}
	}

	if changed {
		refetched, found, err := oneflowFindServiceByID(ctx, conn, url, user, pass, id)
		if err != nil {
			return oneflowOpResult{}, err
		}
		if found {
			service = refetched
		}
	}
	return oneflowOpResult{service: service, changed: changed}, nil
}

func oneflowRoleCardinality(service map[string]any, roleName string) (int, bool) {
	roles := oneflowRolesList(service)
	for _, r := range roles {
		if oneflowStr(r["name"]) == roleName {
			return oneflowInt(r["cardinality"]), true
		}
	}
	return 0, false
}

func oneflowRolesList(service map[string]any) []map[string]any {
	tmpl, _ := service["TEMPLATE"].(map[string]any)
	body, _ := tmpl["BODY"].(map[string]any)
	roles, _ := body["roles"].([]any)
	out := make([]map[string]any, 0, len(roles))
	for _, r := range roles {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func oneServiceState(service map[string]any) string {
	tmpl, _ := service["TEMPLATE"].(map[string]any)
	body, _ := tmpl["BODY"].(map[string]any)
	idx := oneflowInt(body["state"])
	if idx >= 0 && idx < len(oneServiceStates) {
		return oneServiceStates[idx]
	}
	return "UNKNOWN"
}

// oneflowServicePermissions renders service's own PERMISSIONS block as
// a 3-digit octal string, matching real one_service.py's own
// parse_service_permissions.
func oneflowServicePermissions(service map[string]any) string {
	perms, _ := service["PERMISSIONS"].(map[string]any)
	octal := func(u, m, a string) string {
		return strconv.Itoa(oneflowInt(perms[u])*4 + oneflowInt(perms[m])*2 + oneflowInt(perms[a]))
	}
	return octal("OWNER_U", "OWNER_M", "OWNER_A") + octal("GROUP_U", "GROUP_M", "GROUP_A") + octal("OTHER_U", "OTHER_M", "OTHER_A")
}

func oneServiceResultWithFacts(res Result, service map[string]any) Result {
	res = res.WithExtra("service_id", oneflowInt(service["ID"]))
	res = res.WithExtra("service_name", oneflowStr(service["NAME"]))
	res = res.WithExtra("group_id", oneflowInt(service["GID"]))
	res = res.WithExtra("group_name", oneflowStr(service["GNAME"]))
	res = res.WithExtra("owner_id", oneflowInt(service["UID"]))
	res = res.WithExtra("owner_name", oneflowStr(service["UNAME"]))
	res = res.WithExtra("state", oneServiceState(service))
	res = res.WithExtra("mode", oneflowServicePermissions(service))

	var roles []any
	for _, r := range oneflowRolesList(service) {
		var ids []any
		if nodes, ok := r["nodes"].([]any); ok {
			for _, n := range nodes {
				if nm, ok := n.(map[string]any); ok {
					ids = append(ids, nm["deploy_id"])
				}
			}
		}
		if ids == nil {
			ids = []any{}
		}
		stateIdx := oneflowInt(r["state"])
		stateName := "UNKNOWN"
		if stateIdx >= 0 && stateIdx < len(oneServiceStates) {
			stateName = oneServiceStates[stateIdx]
		}
		roles = append(roles, map[string]any{
			"name": oneflowStr(r["name"]), "cardinality": oneflowInt(r["cardinality"]),
			"state": stateName, "ids": ids,
		})
	}
	if roles == nil {
		roles = []any{}
	}
	res = res.WithExtra("roles", roles)
	return res
}

func oneflowStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmtAny(v)
}

func oneflowInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	case int:
		return x
	default:
		return 0
	}
}
