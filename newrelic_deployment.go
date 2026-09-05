package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleNewrelicDeployment implements (a subset of) Ansible's
// `newrelic_deployment` module: records an APM deployment marker, via
// New Relic's own official `newrelic` CLI (newrelic-cli) instead of real
// newrelic_deployment.py's own hand-rolled `fetch_url` calls against the
// legacy v2 REST deployments API — the same "shell out to the
// platform's own official CLI instead of an API client" precedent this
// port already uses elsewhere (see github_common.go's own doc comment
// for the fuller rationale).
//
// Args: token (string, required) — exported as the NEW_RELIC_API_KEY
// environment variable for each single `newrelic` invocation this
// module makes (never as a command-line flag — this project's own hard
// "no secrets in argv" rule), matching newrelic-cli's own documented
// non-interactive-auth environment variable exactly (its own
// GETTING_STARTED.md: "export NEW_RELIC_API_KEY=<your_personal_api_key>"
// works immediately, no prior `newrelic profile add` required). app_name
// (string) or application_id (string) — one is required, matching real
// newrelic_deployment.py's own required_one_of; giving both is a Fail
// (a normal task failure, matching real newrelic_deployment.py's own
// manual `module.fail_json(msg="only one of...")` check, which is not
// expressed as an argument_spec mutually_exclusive either). revision
// (string, required); changelog/description/user (string, optional);
// app_name_exact_match (bool, default false) — requires app_name (an
// argument-validation error otherwise, matching real
// newrelic_deployment.py's own required_if).
//
// # A genuine credential-SHAPE nuance
//
// Real newrelic_deployment.py's own `token` is documented as "API token
// to place in the Api-Key header" — a classic New Relic v2 REST API
// key. `newrelic apm deployment create`/`apm application search` talk
// to New Relic's NerdGraph API instead, which requires a Personal API
// Key — a DIFFERENT credential type in New Relic's own account model
// (verified against newrelic-cli's own GETTING_STARTED.md, which
// documents NEW_RELIC_API_KEY as taking a "Personal API Key", not a v2
// REST API key). A caller must supply a Personal API Key as this
// module's own token argument for `newrelic` to authenticate
// successfully — an honestly-documented credential-shape gap (matching
// hwc_common.go's own AK/SK-vs-Keystone precedent), not a silent
// misinterpretation. account selection likewise is not wired through at
// all (real newrelic_deployment.py has no account argument either): a
// Personal API Key scoped to more than one account may need
// NEW_RELIC_ACCOUNT_ID already set in the target's own environment for
// `newrelic apm application search` to find the right application —
// this port does not set that itself.
//
// # app_name resolution: NerdGraph entity search, not the v2 filter
// # real newrelic_deployment.py's own get_application_id used
//
// `newrelic apm application search --name <app_name> --format json`
// (verified against newrelic-cli's own source, internal/apm/
// command_application.go) runs a NerdGraph entity search scoped to
// domain APM / type APPLICATION and returns a JSON array of entity
// outlines — each one carrying a numeric `applicationId` field
// alongside `name` (verified against newrelic-client-go's own
// ApmApplicationEntityOutline struct), which is exactly the classic
// numeric ID `apm deployment create --applicationId` needs. When
// app_name_exact_match is true, this port picks the first result whose
// own `name` field equals app_name exactly (Fail if none does); when
// false, it takes the first result overall — matching real
// newrelic_deployment.py's own get_application_id logic exactly (loop
// for an exact match vs. `result["applications"][0]`).
func moduleNewrelicDeployment(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, errArg("newrelic_deployment: %v", err)
	}
	revision, err := requireString(args, "revision")
	if err != nil {
		return Result{}, errArg("newrelic_deployment: %v", err)
	}
	appName := argString(args, "app_name", "")
	applicationID := argString(args, "application_id", "")
	exactMatch := argBool(args, "app_name_exact_match", false)
	changelog := argString(args, "changelog", "")
	description := argString(args, "description", "")
	user := argString(args, "user", "")

	if exactMatch && appName == "" {
		return Result{}, errArg("newrelic_deployment: app_name_exact_match requires app_name")
	}
	if appName != "" && applicationID != "" {
		return Fail("newrelic_deployment: only one of 'app_name' or 'application_id' can be set"), nil
	}
	if appName == "" && applicationID == "" {
		return Result{}, errArg("newrelic_deployment: one of app_name or application_id is required")
	}

	if _, err := run(ctx, conn, "command -v newrelic"); err != nil {
		return Fail("newrelic_deployment: the newrelic binary (New Relic's own official CLI, newrelic-cli) is " +
			"required on the target and was not found in PATH — see newrelic_deployment.go's own doc comment"), nil
	}

	appID := applicationID
	if appID == "" {
		id, res, err := newrelicResolveAppID(ctx, conn, token, appName, exactMatch)
		if err != nil {
			return Result{}, err
		}
		if id == "" {
			return Fail(fmt.Sprintf("newrelic_deployment: no application found with name %q: %s", appName, newrelicErrMsg(res))), nil
		}
		appID = id
	}

	argv := []string{"apm", "deployment", "create", "--applicationId", appID, "--revision", revision, "--format", "json"}
	if changelog != "" {
		argv = append(argv, "--change-log", changelog)
	}
	if description != "" {
		argv = append(argv, "--description", description)
	}
	if user != "" {
		argv = append(argv, "--user", user)
	}
	res, err := newrelicRun(ctx, conn, token, argv...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("newrelic_deployment: unable to insert deployment marker: %s", newrelicErrMsg(res))), nil
	}
	return Changed(fmt.Sprintf("deployment marker created for application %s", appID)).WithExtra("application_id", appID), nil
}

func newrelicCmd(token string, argv ...string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return "NEW_RELIC_API_KEY=" + shellQuote(token) + " newrelic " + strings.Join(quoted, " ")
}

func newrelicRun(ctx context.Context, conn remoteexec.Connection, token string, argv ...string) (remoteexec.Result, error) {
	return conn.Exec(ctx, newrelicCmd(token, argv...), nil)
}

// newrelicResolveAppID runs `newrelic apm application search --name
// <appName> --format json` and returns the applicationId of either the
// first exact-name match (exactMatch=true) or the first result overall.
func newrelicResolveAppID(ctx context.Context, conn remoteexec.Connection, token, appName string, exactMatch bool) (string, remoteexec.Result, error) {
	res, err := newrelicRun(ctx, conn, token, "apm", "application", "search", "--name", appName, "--format", "json")
	if err != nil {
		return "", res, err
	}
	if res.RC != 0 {
		return "", res, nil
	}
	var results []map[string]any
	if strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), &results); jerr != nil {
			return "", res, fmt.Errorf("decoding newrelic apm application search output: %w", jerr)
		}
	}
	if len(results) == 0 {
		return "", res, nil
	}
	if exactMatch {
		for _, r := range results {
			if fmt.Sprint(r["name"]) == appName {
				return newrelicApplicationIDOf(r), res, nil
			}
		}
		return "", res, nil
	}
	return newrelicApplicationIDOf(results[0]), res, nil
}

func newrelicApplicationIDOf(r map[string]any) string {
	switch v := r["applicationId"].(type) {
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func newrelicErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
