package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleBigpanda implements Ansible's `bigpanda` (community.general)
// module: notifies BigPanda that a deployment started, finished or
// failed.
//
// BigPanda has no official CLI, so this posts to the same endpoints
// real bigpanda.py's own fetch_url call hits -- POST
// <url>/data/events/deployments/start or .../end, JSON, with an
// Authorization: Bearer header -- through curl ON THE TARGET.
//
// Args: component (required, alias name); version (required); token
// (required); state (started|finished|failed, required); hosts (alias
// host); env; owner; description; deployment_message; source_system
// (default "ansible"); url (default https://api.bigpanda.io).
//
// # start and end take different bodies
//
// state=started posts to /start and adds source_system, env, owner and
// description. finished and failed post to /end instead and add
// status=success or status=failure, plus errorMessage from
// deployment_message. Reproduced, including that the extra start fields
// are NOT sent on an end event.
//
// # hosts defaults to the target's own hostname
//
// Real defaults it to socket.gethostname(), and real's module runs on
// the target -- so that is the TARGET's hostname, not the controller's.
// This asks the target, through the same connection the request goes
// out on, rather than using the controller's name and quietly reporting
// the wrong host. A single host is wrapped into a list, as real does.
//
// # The token is left out of the result
//
// ⛔ Real builds `deployment = dict(token=token, url=url)` and returns
// it with exit_json, so the token is a result FIELD -- in check mode and
// on success alike. Real can do that because AnsibleModule's
// _return_formatted calls remove_values, which replaces every no_log
// value in the result with "********". This port has no such scrubber,
// so the token is omitted from the result and the url, state and body
// fields are reported instead. Same reasoning as telegram's omitted
// url: real can afford a result this port cannot.
func moduleBigpanda(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	component := argString(args, "component", argString(args, "name", ""))
	if component == "" {
		return Result{}, errArg("missing required argument: component")
	}
	version, err := requireString(args, "version")
	if err != nil {
		return Result{}, err
	}
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	switch state {
	case "started", "finished", "failed":
	default:
		return Fail("bigpanda: value of state must be one of: started, finished, failed, got: " + state), nil
	}
	base := argString(args, "url", "https://api.bigpanda.io")

	body := map[string]any{"component": component, "version": version}

	hosts := argStringList(args, "hosts")
	if len(hosts) == 0 {
		hosts = argStringList(args, "host")
	}
	if len(hosts) == 0 {
		// Real's socket.gethostname(), asked of the TARGET -- which is
		// where real's own module runs.
		out, err := run(ctx, conn, "hostname")
		if err != nil {
			return Fail("bigpanda: determining the target's hostname: " + err.Error()), nil
		}
		hosts = []string{strings.TrimSpace(out)}
	}
	body["hosts"] = hosts

	requestURL := base + "/data/events/deployments/start"
	if state == "started" {
		for _, k := range []string{"source_system", "env", "owner", "description"} {
			def := ""
			if k == "source_system" {
				def = "ansible"
			}
			if v := argString(args, k, def); v != "" {
				body[k] = v
			}
		}
	} else {
		requestURL = base + "/data/events/deployments/end"
		if m := argString(args, "deployment_message", ""); m != "" {
			body["errorMessage"] = m
		}
		if state == "finished" {
			body["status"] = "success"
		} else {
			body["status"] = "failure"
		}
	}

	// Real's own result shape, minus the token -- see the doc comment.
	// Real's deployment object is dict(token, url) + body, with
	// errorMessage renamed -- there is no "state" key in it, however
	// natural one would look.
	result := Changed("").WithExtra("url", base)
	for k, v := range body {
		key := k
		if k == "errorMessage" {
			// Real renames it on the way out.
			key = "message"
		}
		result = result.WithExtra(key, v)
	}

	if InCheckMode(args) {
		// Real reports changed=True here and sends nothing.
		return result, nil
	}

	if res, ok := requireCurl(ctx, conn, "bigpanda", "BigPanda"); !ok {
		return res, nil
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return Fail("bigpanda: encoding body: " + err.Error()), nil
	}
	respBody, status, err := curlDo(ctx, conn, curlReq{
		Method: "POST", URL: requestURL, JSON: string(encoded),
		Headers: [][2]string{
			{"Authorization", "Bearer " + token},
			{"Content-Type", "application/json"},
		},
	})
	if err != nil {
		return Fail("bigpanda: " + err.Error()), nil
	}
	if status != 200 {
		return Fail(fmt.Sprintf("bigpanda: HTTP %d: %s", status, strings.TrimSpace(respBody))), nil
	}
	return result, nil
}
