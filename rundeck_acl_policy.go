package modules

import (
	"context"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"

	yaml "gopkg.in/yaml.v3"
)

// moduleRundeckACLPolicy implements Ansible's `rundeck_acl_policy`
// module: creates, updates, or removes a Rundeck ACL policy (a system
// policy when `project` is unset, a project policy otherwise), via the
// `rd` CLI (see rundeck_common.go's own doc comment).
//
// Args: name (required); state (present|absent, default "present");
// project (string, optional — system-scoped when empty); policy
// (required when state=present) — real rundeck_acl_policy declares
// this `type: str` but its own doc says "It can be a YAML string or a
// pure Ansible inventory YAML object"; since this port's own args map
// preserves whatever shape a task's YAML actually parsed to, a
// non-string policy value (a map or list, i.e. a native YAML mapping
// written directly under `policy:`) is YAML-encoded via gopkg.in/
// yaml.v3 before being sent — a string value is sent verbatim, never
// re-encoded. url (required); api_token (required, aliased token).
//
// Real rundeck_acl_policy's own name validation
// (`re.match("[a-zA-Z0-9,.+_-]+", name)`) is NOT anchored at the end
// (Python's re.match only anchors at the START), so it only actually
// requires name's FIRST character to be in that set — this port
// faithfully reproduces that exact narrow check rather than the
// stricter whole-string validation the error message ("contains
// forbidden characters") implies real rundeck_acl_policy performs.
//
// Existence and current content are probed via `rd system acls get -n
// <name>` / `rd projects acls get -p <project> -n <name>` — this
// port's own before/after Extra fields hold that command's raw stdout
// (the ACL policy file's own YAML text) directly, NOT the `{"contents":
// "..."}` JSON envelope real rundeck_acl_policy's own before/after
// return values carry: `rd`'s own get/show subcommands for ACL content
// print the policy file's raw text (their whole documented purpose is
// downloading a policy for editing), not a JSON-wrapped API response —
// an architecture-driven, honestly-documented shape deviation, not a
// guess.
//
// state=present, policy absent: create (`... acls create -n <name>
// [-p <project>] -f -`, policy text piped over stdin).
// state=present, policy exists, content unchanged (exact string
// compare against the trimmed current text, matching real
// rundeck_acl_policy's own exact `facts["contents"] ==
// module.params["policy"]` comparison): no-op.
// state=present, policy exists, content different: update (same verb,
// `update` instead of `create`).
// state=absent, policy exists: delete.
// state=absent, policy absent: no-op.
func moduleRundeckACLPolicy(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	if !rundeckACLNameRe.MatchString(name) {
		return Result{}, errArg("rundeck_acl_policy: Name contains forbidden characters. The policy can contain the characters: a-zA-Z0-9,.+_-")
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent":
	default:
		return Result{}, errArg("rundeck_acl_policy: state must be one of present, absent, got %q", state)
	}
	project := argString(args, "project", "")
	url, token, err := rdAuth(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := rdRequireBinary(ctx, conn, "rundeck_acl_policy"); !ok {
		return res, nil
	}

	var policyText string
	if state == "present" {
		policyText, err = rundeckACLPolicyText(args)
		if err != nil {
			return Result{}, err
		}
		if strings.TrimSpace(policyText) == "" {
			return Result{}, errArg("rundeck_acl_policy: policy is required when state is present")
		}
	}

	target := rundeckACLTarget{project: project, name: name}
	before, found, err := rundeckACLGet(ctx, conn, url, token, target)
	if err != nil {
		return Result{}, err
	}

	switch {
	case state == "present" && !found:
		res, err := rundeckACLMutate(ctx, conn, url, token, "create", target, policyText)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("rundeck_acl_policy: creating ACL: "+rdErrMsg(res)).WithExtra("rundeck_response", rdErrMsg(res)), nil
		}
		out := Changed("")
		return out.WithExtra("before", "").WithExtra("after", policyText), nil

	case state == "present" && found:
		if before == strings.TrimSpace(policyText) {
			res := Ok("")
			return res.WithExtra("before", before).WithExtra("after", before), nil
		}
		res, err := rundeckACLMutate(ctx, conn, url, token, "update", target, policyText)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("rundeck_acl_policy: updating ACL: "+rdErrMsg(res)).WithExtra("rundeck_response", rdErrMsg(res)), nil
		}
		out := Changed("")
		return out.WithExtra("before", before).WithExtra("after", policyText), nil

	case state == "absent" && !found:
		res := Ok("")
		return res.WithExtra("before", "").WithExtra("after", ""), nil

	default: // state == "absent" && found
		res, err := rundeckACLDelete(ctx, conn, url, token, target)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("rundeck_acl_policy: deleting ACL: "+rdErrMsg(res)).WithExtra("rundeck_response", rdErrMsg(res)), nil
		}
		out := Changed("")
		return out.WithExtra("before", before).WithExtra("after", ""), nil
	}
}

var rundeckACLNameRe = regexp.MustCompile(`^[a-zA-Z0-9,.+_-]`)

type rundeckACLTarget struct {
	project string
	name    string
}

func (t rundeckACLTarget) argv(verb string) []string {
	var argv []string
	if t.project != "" {
		argv = []string{"projects", "acls", verb, "-p", t.project, "-n", t.name}
	} else {
		argv = []string{"system", "acls", verb, "-n", t.name}
	}
	return argv
}

func rundeckACLGet(ctx context.Context, conn remoteexec.Connection, url, token string, t rundeckACLTarget) (string, bool, error) {
	res, err := rdRun(ctx, conn, url, token, t.argv("get")...)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	return strings.TrimSpace(res.Stdout), true, nil
}

func rundeckACLMutate(ctx context.Context, conn remoteexec.Connection, url, token, verb string, t rundeckACLTarget, policyText string) (remoteexec.Result, error) {
	argv := append(t.argv(verb), "-f", "-")
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := "RD_FORMAT=json"
	if url != "" {
		cmd += " RD_URL=" + shellQuote(url)
	}
	if token != "" {
		cmd += " RD_TOKEN=" + shellQuote(token)
	}
	cmd += " rd " + strings.Join(quoted, " ")
	return conn.Exec(ctx, cmd, strings.NewReader(policyText))
}

func rundeckACLDelete(ctx context.Context, conn remoteexec.Connection, url, token string, t rundeckACLTarget) (remoteexec.Result, error) {
	return rdRun(ctx, conn, url, token, t.argv("delete")...)
}

// rundeckACLPolicyText renders args["policy"] to text: a string value
// passes through verbatim; any other value (a native YAML mapping/list)
// is YAML-encoded — see this file's own doc comment.
func rundeckACLPolicyText(args map[string]any) (string, error) {
	v, ok := args["policy"]
	if !ok || v == nil {
		return "", nil
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", errArg("rundeck_acl_policy: encoding policy as YAML: %v", err)
	}
	return string(b), nil
}
