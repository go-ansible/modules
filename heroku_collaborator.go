package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHerokuCollaborator implements (a subset of) Ansible's
// `heroku_collaborator` module: adds or removes a Heroku app
// collaborator, via Heroku's own official `heroku` CLI instead of the
// `heroku3` Python API client real heroku_collaborator.py uses — the
// same "shell out to the platform's own official CLI instead of an API
// client" precedent this port already uses elsewhere (see
// github_common.go's own doc comment for the fuller rationale).
//
// Collaborator management lives under the CLI's `access` command
// family, NOT `apps:collaborators` (which does not exist): `heroku
// access -a <app> --json` lists collaborators, `heroku access:add
// EMAIL -a <app> [-p <permissions>]` adds one, `heroku access:remove
// EMAIL -a <app>` removes one — verified against heroku/cli's own
// generated command reference (docs/access.md), not guessed from the
// module name.
//
// Args: user (string, required) — collaborator email or user ID; apps
// ([]string, required); state (present|absent, default "present");
// api_key (string, optional) — exported as the HEROKU_API_KEY
// environment variable for each single `heroku` invocation this call
// makes (never as a command-line flag — this project's own hard "no
// secrets in argv" rule), matching real heroku_collaborator.py's own
// documented HEROKU_API_KEY/TF_VAR_HEROKU_API_KEY fallback exactly:
// unlike several other CLI-substitution modules in this port, this one
// genuinely wires its own credential argument through, because `heroku`
// itself already reads that same environment variable as its own
// documented non-interactive auth path. When api_key is empty, `heroku`
// must already be authenticated on the target (a prior `heroku login`,
// or HEROKU_API_KEY already exported in the invoking shell's own
// environment) — this port does not drive an interactive login itself.
//
// suppress_invitation (bool, default false) is accepted for
// argument-shape compatibility with real playbooks but has NO EFFECT
// here: real heroku_collaborator.py's own "silent" behavior comes from
// a heroku3-library-only API field with no equivalent flag on `heroku
// access:add` at all (verified against its own generated reference,
// which documents no notification-suppression flag) — a documented gap,
// not a silent misinterpretation. This port likewise never passes a
// `-p/--permissions` flag on `access:add` — real heroku_collaborator.py
// has no permission argument either (it always grants whatever
// `access:add` defaults to, unset), so this matches rather than
// deviates.
//
// Idempotency: for each app, `heroku access -a <app> --json` is decoded
// and searched for user's own email among each entry's `user.email`
// field (falling back to a top-level `email` field, in case of a
// differently-shaped response) — present when found, absent otherwise,
// matching real heroku_collaborator.py's own
// `[c.user.email for c in heroku_app.collaborators()]` check. An app
// that does not exist (`heroku access` exits non-zero) is reported as a
// Fail, matching real heroku_collaborator.py's own
// `module.fail_json(msg=f"App {app} does not exist")`.
func moduleHerokuCollaborator(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	user, err := requireString(args, "user")
	if err != nil {
		return Result{}, errArg("heroku_collaborator: %v", err)
	}
	apps := argStringList(args, "apps")
	if len(apps) == 0 {
		return Result{}, errArg("heroku_collaborator: missing required argument: apps")
	}
	state := argString(args, "state", "present")
	apiKey := argString(args, "api_key", "")

	var affected []string
	for _, app := range apps {
		collaborators, res, err := herokuAccessList(ctx, conn, apiKey, app)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(fmt.Sprintf("heroku_collaborator: app %s does not exist or is not accessible: %s", app, herokuErrMsg(res))), nil
		}
		present := herokuCollaboratorPresent(collaborators, user)

		switch state {
		case "absent":
			if !present {
				continue
			}
			if _, err := herokuRun(ctx, conn, apiKey, "access:remove", user, "-a", app); err != nil {
				return Result{}, err
			}
			affected = append(affected, app)
		default: // "present"
			if present {
				continue
			}
			if _, err := herokuRun(ctx, conn, apiKey, "access:add", user, "-a", app); err != nil {
				return Result{}, err
			}
			affected = append(affected, app)
		}
	}

	if len(affected) == 0 {
		return Ok("no change").WithExtra("apps", []string{}), nil
	}
	return Changed(fmt.Sprintf("%s %s on %d app(s)", user, state, len(affected))).WithExtra("apps", affected), nil
}

// herokuCmd renders one `heroku` invocation, shell-quoting each argv
// entry, prefixed with HEROKU_API_KEY=<key> when apiKey is non-empty
// (see moduleHerokuCollaborator's own doc comment on why this is safe
// to wire through as an env var for a single command).
func herokuCmd(apiKey string, argv ...string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := "heroku " + strings.Join(quoted, " ")
	if apiKey != "" {
		cmd = "HEROKU_API_KEY=" + shellQuote(apiKey) + " " + cmd
	}
	return cmd
}

func herokuRun(ctx context.Context, conn remoteexec.Connection, apiKey string, argv ...string) (remoteexec.Result, error) {
	return conn.Exec(ctx, herokuCmd(apiKey, argv...), nil)
}

// herokuAccessList runs `heroku access -a <app> --json` and decodes its
// stdout as a JSON array of collaborator objects.
func herokuAccessList(ctx context.Context, conn remoteexec.Connection, apiKey, app string) ([]map[string]any, remoteexec.Result, error) {
	res, err := herokuRun(ctx, conn, apiKey, "access", "-a", app, "--json")
	if err != nil {
		return nil, res, err
	}
	if res.RC != 0 || strings.TrimSpace(res.Stdout) == "" {
		return nil, res, nil
	}
	var list []map[string]any
	if jerr := json.Unmarshal([]byte(res.Stdout), &list); jerr != nil {
		return nil, res, fmt.Errorf("decoding heroku access --json output for app %s: %w", app, jerr)
	}
	return list, res, nil
}

// herokuCollaboratorPresent reports whether email appears as any entry's
// user.email (or a flat top-level email, as a fallback for a
// differently-shaped response).
func herokuCollaboratorPresent(collaborators []map[string]any, email string) bool {
	for _, c := range collaborators {
		if u, ok := c["user"].(map[string]any); ok {
			if fmt.Sprint(u["email"]) == email {
				return true
			}
		}
		if fmt.Sprint(c["email"]) == email {
			return true
		}
	}
	return false
}

// herokuErrMsg prefers a failed `heroku` invocation's own stderr,
// falling back to stdout.
func herokuErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
