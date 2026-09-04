package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRundeckProject implements Ansible's `rundeck_project` module:
// creates or removes a Rundeck project, via the `rd` CLI (see
// rundeck_common.go's own doc comment for the `gh`/`glab`-style CLI
// substitution this batch makes, and for how url/api_token become
// RD_URL/RD_TOKEN).
//
// Args: name (required); state (present|absent, default "present");
// url (required); api_token (required, aliased token) — see
// rundeck_common.go. url_username/url_password/client_cert/client_key/
// force/force_basic_auth/http_agent/use_gssapi/use_proxy/
// validate_certs are all accepted (argument-shape compatibility) but
// have NO EFFECT — see rundeck_common.go's own doc comment.
//
// Real rundeck_project's own EXAMPLES show label/description
// arguments, but — verified directly against real rundeck_project.py's
// own argument_spec (which declares only state/name plus the shared
// api_argument_spec connection args) — those are NOT real accepted
// arguments at all; the example itself is stale/wrong in upstream's own
// docs. This port matches the real argument_spec, not the misleading
// example: it accepts only name/state (see rundeck_common.go's own
// noted approach of reading source over guessing from an example).
//
// Existence is probed via `rd projects info -p <name>`, matching real
// rundeck_project's own GET project/{name}. A non-zero exit is treated
// as "project does not exist" uniformly — real rundeck_project's own
// api_request distinguishes a 404 (not found, not a failure) from
// other HTTP errors (403 token-forbidden, 5xx) and fails hard on the
// latter; this port cannot make that same distinction from `rd`'s own
// exit code and stderr text alone without a live `rd` binary to pin
// the wording against, so any info failure here is read as "absent" —
// a documented simplification, not a silent one.
//
// state=present, project already exists: no-op (real rundeck_project
// does not update an existing project's config either — its own
// create_or_update_project only creates, matching the function's
// misleading name).
//
// state=present, project absent: `rd projects create -p <name>`.
// state=absent, project exists: `rd projects delete -p <name>`.
// state=absent, project absent: no-op.
//
// Extra fields "before"/"after" mirror real rundeck_project's own
// identically-named return values (a JSON object, or an empty object
// when the project did not exist on that side of the operation).
func moduleRundeckProject(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent":
	default:
		return Result{}, errArg("rundeck_project: state must be one of present, absent, got %q", state)
	}
	url, token, err := rdAuth(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := rdRequireBinary(ctx, conn, "rundeck_project"); !ok {
		return res, nil
	}

	before, found, err := rundeckProjectInfo(ctx, conn, url, token, name)
	if err != nil {
		return Result{}, err
	}

	switch {
	case state == "present" && found:
		res := Ok("")
		return res.WithExtra("before", before).WithExtra("after", before), nil

	case state == "present" && !found:
		res, err := rdRun(ctx, conn, url, token, "projects", "create", "-p", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("rundeck_project: creating project: "+rdErrMsg(res)).WithExtra("rundeck_response", rdErrMsg(res)), nil
		}
		after, _, err := rundeckProjectInfo(ctx, conn, url, token, name)
		if err != nil {
			return Result{}, err
		}
		out := Changed("")
		return out.WithExtra("before", map[string]any{}).WithExtra("after", after), nil

	case state == "absent" && !found:
		res := Ok("")
		return res.WithExtra("before", map[string]any{}).WithExtra("after", map[string]any{}), nil

	default: // state == "absent" && found
		res, err := rdRun(ctx, conn, url, token, "projects", "delete", "-p", name)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("rundeck_project: deleting project: "+rdErrMsg(res)).WithExtra("rundeck_response", rdErrMsg(res)), nil
		}
		out := Changed("")
		return out.WithExtra("before", before).WithExtra("after", map[string]any{}), nil
	}
}

func rundeckProjectInfo(ctx context.Context, conn remoteexec.Connection, url, token, name string) (map[string]any, bool, error) {
	var out map[string]any
	res, err := rdRunJSON(ctx, conn, url, token, &out, "projects", "info", "-p", name)
	if err != nil {
		return nil, false, err
	}
	if res.RC != 0 || out == nil {
		return nil, false, nil
	}
	return out, true, nil
}
