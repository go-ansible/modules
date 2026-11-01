package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// PagerDuty has three modules in community.general that talk plain REST
// through fetch_url, and one that does not. The three are here; the
// fourth, pagerduty_user, is deliberately absent -- see the note at the
// bottom of this file.
//
// All three reach api.pagerduty.com or events.pagerduty.com with curl ON
// THE TARGET, the pattern the other HTTP modules in this package use,
// with the url, the headers and the body on curl's stdin config so no
// token reaches argv.
//
// # One thing none of them can reproduce exactly
//
// Real's failure messages interpolate fetch_url's own info['msg'] -- a
// string made by urllib and Ansible's url helper, not by the service.
// There is no way to produce that text from curl, so each message below
// carries the HTTP status and the service's own trimmed body instead.
// The sentence around it is real's, word for word, including the missing
// space in "status.Reason" that real's pagerduty_alert really has.

// pagerDutyAPIHeaders are the headers real's api.pagerduty.com callers
// send: the REST API needs the Accept header to pin v2, and the token is
// a header rather than a bearer or a query parameter.
func pagerDutyAPIHeaders(token string) [][2]string {
	return [][2]string{
		{"Content-Type", "application/json"},
		{"Authorization", "Token token=" + token},
		{"Accept", "application/vnd.pagerduty+json;version=2"},
	}
}

// modulePagerdutyChange implements Ansible's `pagerduty_change`
// (community.general) module: records a change event, the
// deploy-happened signal that shows up on a PagerDuty service timeline.
//
// Args: integration_key (required); summary (required); source (default
// "Ansible"); user; repo; revision; environment; link_url; link_text;
// url (default https://events.pagerduty.com/v2/change/enqueue).
//
// user, repo, revision and environment go into custom_details, each only
// when non-empty, which is real's own truthiness test rather than a
// presence test -- an explicitly empty repo: is omitted, not sent empty.
//
// # Check mode POSTS, and expects to be refused
//
// Real's dry run is not a skip and not a GET: it POSTs to the same
// endpoint with NO body, and treats HTTP 400 as success, because a
// reachable change-events endpoint rejects an empty event. Anything else
// -- including a 202 -- fails with real's own sentence. That probe is
// reproduced here: it tells the operator the key's endpoint answers,
// without enqueueing a change.
//
// Success is 202 and reports changed; real's failure sentence names the
// status it got.
func modulePagerdutyChange(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "integration_key")
	if err != nil {
		return Result{}, err
	}
	summary, err := requireString(args, "summary")
	if err != nil {
		return Result{}, err
	}
	endpoint := argString(args, "url", "https://events.pagerduty.com/v2/change/enqueue")
	headers := [][2]string{{"Content-Type", "application/json"}}

	if res, ok := requireCurl(ctx, conn, "pagerduty_change", "PagerDuty"); !ok {
		return res, nil
	}

	if InCheckMode(args) {
		// Real's own probe: an empty POST, and 400 is the pass.
		_, status, err := curlDo(ctx, conn, curlReq{Method: "POST", URL: endpoint, Headers: headers})
		if err != nil {
			return Fail("pagerduty_change: " + err.Error()), nil
		}
		if status == 400 {
			return Changed(""), nil
		}
		return Fail(fmt.Sprintf(
			"Checking the PagerDuty change event API returned an unexpected response: %d", status)), nil
	}

	details := map[string]any{}
	for _, k := range []string{"user", "repo", "revision", "environment"} {
		if v := argString(args, k, ""); v != "" {
			details[k] = v
		}
	}
	payload := map[string]any{
		"summary": summary,
		"source":  argString(args, "source", "Ansible"),
		// Real: now().strftime("%Y-%m-%dT%H:%M:%S.%fZ") on an aware UTC
		// clock. Python's %f is six digits, always printed.
		"timestamp":      time.Now().UTC().Format("2006-01-02T15:04:05.000000Z"),
		"custom_details": details,
	}
	event := map[string]any{"routing_key": key, "payload": payload}
	if href := argString(args, "link_url", ""); href != "" {
		link := map[string]any{"href": href}
		if text := argString(args, "link_text", ""); text != "" {
			link["text"] = text
		}
		event["links"] = []any{link}
	}

	body, err := json.Marshal(event)
	if err != nil {
		return Fail("pagerduty_change: encoding event: " + err.Error()), nil
	}
	_, status, err := curlDo(ctx, conn, curlReq{
		Method: "POST", URL: endpoint, JSON: string(body), Headers: headers,
	})
	if err != nil {
		return Fail("pagerduty_change: " + err.Error()), nil
	}
	if status != 202 {
		return Fail(fmt.Sprintf("Creating PagerDuty change event failed with %d", status)), nil
	}
	return Changed(""), nil
}

// modulePagerdutyAlert implements Ansible's `pagerduty_alert`
// (community.general) module: triggers, acknowledges or resolves an
// incident.
//
// Args: state (required: triggered|acknowledged|resolved); api_version
// (v1|v2, default v1); integration_key or service_key (one required);
// api_key and service_id (required for v1); incident_key (required for
// acknowledged and resolved); name; client; client_url; desc (default
// "Created via Ansible"); and, for v2, component, custom_details,
// incident_class, link_url, link_text, source, severity (default
// critical).
//
// # The two API versions are two different modules wearing one name
//
// v1 (the default) LOOKS BEFORE IT WRITES: it GETs
// api.pagerduty.com/incidents for the service, decides from the newest
// incident's status whether anything would change, and only then POSTs
// the old generic/2010-04-15 event. So v1 is idempotent and needs
// api_key and service_id on top of the integration key.
//
// v2 posts to events.pagerduty.com/v2/enqueue and reports changed
// unconditionally -- it has nothing to compare against. It also DROPS
// the payload for acknowledge and resolve, because those carry only the
// dedup_key, and it refuses a trigger with no source using real's own
// sentence, which says "service" where the argument is called source.
//
// service_key is real's obsolete spelling of integration_key and warns.
//
// # A deliberate divergence in check mode
//
// Real runs the v2 branch in check mode too: module.check_mode guards
// only the v1 POST, so `--check` with api_version: v2 really pages
// somebody. This port does not do that -- a dry run that wakes an
// on-call engineer is not a dry run -- and reports what it would have
// sent instead. v1's check mode behaves as real's does: the GET runs,
// the POST does not. Neither reaches a playbook today, since
// pagerduty_alert is not in checkModeSupport and the engine skips it.
func modulePagerdutyAlert(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	eventType, ok := map[string]string{
		"triggered":    "trigger",
		"acknowledged": "acknowledge",
		"resolved":     "resolve",
	}[state]
	if !ok {
		return Fail("pagerduty_alert: value of state must be one of: triggered, acknowledged, resolved, got: " + state), nil
	}
	apiVersion := argString(args, "api_version", "v1")
	if apiVersion != "v1" && apiVersion != "v2" {
		return Fail("pagerduty_alert: value of api_version must be one of: v1, v2, got: " + apiVersion), nil
	}

	var warnings []string
	integrationKey := argString(args, "integration_key", "")
	if integrationKey == "" {
		integrationKey = argString(args, "service_key", "")
		if integrationKey != "" {
			// Real's own wording, emitted whenever the fallback is used.
			warnings = append(warnings,
				`"service_key" is obsolete parameter and will be removed. Please, use "integration_key" instead`)
		}
	}
	if integrationKey == "" {
		return Fail("pagerduty_alert: one of the following is required: service_key, integration_key"), nil
	}
	// Real's required_if, which the framework enforces before the module
	// body runs.
	incidentKey := argString(args, "incident_key", "")
	if (state == "acknowledged" || state == "resolved") && incidentKey == "" {
		return Fail("pagerduty_alert: state is " + state + " but all of the following are missing: incident_key"), nil
	}
	apiKey := argString(args, "api_key", "")
	serviceID := argString(args, "service_id", "")
	if apiVersion == "v1" && (apiKey == "" || serviceID == "") {
		return Fail("pagerduty_alert: api_version is v1 but all of the following are missing: service_id, api_key"), nil
	}

	desc := argString(args, "desc", "Created via Ansible")
	client := argString(args, "client", "")
	clientURL := argString(args, "client_url", "")

	if res, ok := requireCurl(ctx, conn, "pagerduty_alert", "PagerDuty"); !ok {
		return res, nil
	}

	if apiVersion == "v1" {
		out, changed, res, err := pagerDutyCheckIncident(ctx, conn, state, serviceID, apiKey, incidentKey)
		if err != nil || res.Failed {
			return res, err
		}
		if changed && !InCheckMode(args) {
			body, status, err := curlDo(ctx, conn, curlReq{
				Method: "POST",
				URL:    "https://events.pagerduty.com/generic/2010-04-15/create_event.json",
				JSON: jsonOrEmpty(map[string]any{
					"service_key":  integrationKey,
					"event_type":   eventType,
					"incident_key": nilIfEmpty(incidentKey),
					"description":  desc,
					"client":       nilIfEmpty(client),
					"client_url":   nilIfEmpty(clientURL),
				}),
				Headers: [][2]string{{"Content-type", "application/json"}},
			})
			if err != nil {
				return Fail("pagerduty_alert: " + err.Error()), nil
			}
			if status != 200 {
				return pagerDutyFail(fmt.Sprintf("failed to %s.", eventType), status, body), nil
			}
			out = decodeJSONOr(body, body)
		}
		return pagerDutyWarnings(resultFor(changed).WithExtra("result", out), warnings), nil
	}

	// v2
	payload := map[string]any{
		"summary": desc,
		"source":  nilIfEmpty(argString(args, "source", "")),
		// Real: datetime.now().isoformat() -- the LOCAL clock, with no
		// zone suffix, unlike pagerduty_change's UTC "...Z".
		"timestamp":      time.Now().Format("2006-01-02T15:04:05.000000"),
		"severity":       argString(args, "severity", "critical"),
		"component":      nilIfEmpty(argString(args, "component", "")),
		"class":          nilIfEmpty(argString(args, "incident_class", "")),
		"custom_details": args["custom_details"],
	}
	if sev := argString(args, "severity", "critical"); !slices.Contains([]string{"critical", "warning", "error", "info"}, sev) {
		return Fail("pagerduty_alert: value of severity must be one of: critical, warning, error, info, got: " + sev), nil
	}
	if eventType == "trigger" && argString(args, "source", "") == "" {
		// Real's sentence, which names "service" for the source argument.
		return Fail(`"service" is a required variable for v2 api endpoint.`), nil
	}
	event := map[string]any{
		"routing_key":  integrationKey,
		"event_action": eventType,
		"client":       nilIfEmpty(client),
		"client_url":   nilIfEmpty(clientURL),
	}
	if eventType == "trigger" {
		// Real pops the payload for acknowledge and resolve.
		event["payload"] = payload
	}
	if href := argString(args, "link_url", ""); href != "" {
		link := map[string]any{"href": href}
		if text := argString(args, "link_text", ""); text != "" {
			link["text"] = text
		}
		event["links"] = []any{link}
	}
	if incidentKey != "" {
		event["dedup_key"] = incidentKey
	}

	if InCheckMode(args) {
		// The divergence explained above: real would send this.
		return pagerDutyWarnings(Changed("").
			WithExtra("result", "not sent: a dry run must not page an on-call engineer").
			WithExtra("event_action", eventType), warnings), nil
	}

	body, status, err := curlDo(ctx, conn, curlReq{
		Method: "POST", URL: "https://events.pagerduty.com/v2/enqueue",
		JSON: jsonOrEmpty(event), Headers: [][2]string{{"Content-type", "application/json"}},
	})
	if err != nil {
		return Fail("pagerduty_alert: " + err.Error()), nil
	}
	if status != 202 {
		return pagerDutyFail(fmt.Sprintf("failed to %s.", eventType), status, body), nil
	}
	return pagerDutyWarnings(Changed("").WithExtra("result", decodeJSONOr(body, body)), warnings), nil
}

// pagerDutyCheckIncident is real's v1 `check`: it asks the REST API for
// the service's incidents, newest first, and answers whether the asked
// state would change anything.
//
// Real's rules, kept exactly: no incidents at all means nothing to
// acknowledge or resolve (changed=false) but a trigger WOULD change
// something (changed=true); otherwise the newest incident's status is
// compared with the asked state.
func pagerDutyCheckIncident(ctx context.Context, conn remoteexec.Connection, state, serviceID, apiKey, incidentKey string) (any, bool, Result, error) {
	query := [][2]string{
		{"service_ids[]", serviceID},
		{"sort_by", "incident_number:desc"},
		{"time_zone", "UTC"},
	}
	if incidentKey != "" {
		query = append(query, [2]string{"incident_key", incidentKey})
	}
	body, status, err := curlDo(ctx, conn, curlReq{
		Method: "GET", URL: "https://api.pagerduty.com/incidents",
		Query: query, Headers: pagerDutyAPIHeaders(apiKey),
	})
	if err != nil {
		return nil, false, Fail("pagerduty_alert: " + err.Error()), nil
	}
	if status != 200 {
		// Real's sentence, missing space after the period included.
		return nil, false, pagerDutyFail("failed to check current incident status.", status, body), nil
	}
	var answer struct {
		Incidents []map[string]any `json:"incidents"`
	}
	if err := json.Unmarshal([]byte(body), &answer); err != nil {
		return nil, false, Fail("pagerduty_alert: PagerDuty's incident list is not JSON: " + err.Error()), nil
	}
	if len(answer.Incidents) == 0 {
		const msg = "No corresponding incident"
		return msg, state == "triggered", Result{}, nil
	}
	newest := answer.Incidents[0]
	if s, _ := newest["status"].(string); s != state {
		return newest, true, Result{}, nil
	}
	return newest, false, Result{}, nil
}

// modulePagerduty implements Ansible's `pagerduty` (community.general)
// module: opens, lists or closes a maintenance window, which is how a
// service is silenced while it is deliberately down.
//
// Args: state (required: running|started|ongoing|absent); token
// (required); service (list, alias services); window_id; requester_id;
// hours (default "1"); minutes (default "0"); desc (default "Created by
// Ansible"); name; user.
//
// running and started are the same thing -- real accepts both and
// creates a window either way. ongoing only LISTS, with
// ?filter=ongoing, and reports changed=false. absent deletes by
// window_id.
//
// The window's start and end are computed HERE, on the control node,
// from hours and minutes, exactly as real computes them on the machine
// it runs on: start = now, end = now + hours + minutes, both
// "%Y-%m-%dT%H:%M:%SZ" in UTC. A target whose clock disagrees with ours
// gets our times, which is also true of real whenever the controller is
// not the target.
//
// requester_id travels as the From header and real refuses to create a
// window without it. Each status code is real's own: 200 for the list,
// 201 for the create, 204 for the delete.
func modulePagerduty(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	headers := pagerDutyAPIHeaders(token)

	if res, ok := requireCurl(ctx, conn, "pagerduty", "PagerDuty"); !ok {
		return res, nil
	}

	switch state {
	case "running", "started":
		services := argStringList(args, "service")
		if len(services) == 0 {
			services = argStringList(args, "services")
		}
		if len(services) == 0 {
			return Fail("service not specified"), nil
		}
		requester := argString(args, "requester_id", "")
		if requester == "" {
			return Fail("requester_id is required when maintenance window should be created"), nil
		}
		hours := argString(args, "hours", "1")
		minutes := argString(args, "minutes", "0")
		d, err := time.ParseDuration(hours + "h" + minutes + "m")
		if err != nil {
			return Fail("pagerduty: hours and minutes must be numbers: " + err.Error()), nil
		}
		start := time.Now().UTC()
		refs := make([]any, 0, len(services))
		for _, s := range services {
			refs = append(refs, map[string]any{"id": s, "type": "service_reference"})
		}
		window := map[string]any{"maintenance_window": map[string]any{
			"start_time":  start.Format("2006-01-02T15:04:05Z"),
			"end_time":    start.Add(d).Format("2006-01-02T15:04:05Z"),
			"description": argString(args, "desc", "Created by Ansible"),
			"services":    refs,
		}}
		body, status, err := curlDo(ctx, conn, curlReq{
			Method: "POST", URL: "https://api.pagerduty.com/maintenance_windows",
			JSON:    jsonOrEmpty(window),
			Headers: append(headers, [2]string{"From", requester}),
		})
		if err != nil {
			return Fail("pagerduty: " + err.Error()), nil
		}
		if status != 201 {
			return pagerDutyFail("failed to create the window:", status, body), nil
		}
		return Changed("success").WithExtra("result", decodeJSONOr(body, "")), nil

	case "ongoing":
		body, status, err := curlDo(ctx, conn, curlReq{
			Method: "GET", URL: "https://api.pagerduty.com/maintenance_windows",
			Query: [][2]string{{"filter", "ongoing"}}, Headers: headers,
		})
		if err != nil {
			return Fail("pagerduty: " + err.Error()), nil
		}
		if status != 200 {
			return pagerDutyFail("failed to lookup the ongoing window:", status, body), nil
		}
		// Real reports changed=False for a pure lookup.
		return Ok("success").WithExtra("result", decodeJSONOr(body, "")), nil

	case "absent":
		windowID, err := requireString(args, "window_id")
		if err != nil {
			return Result{}, err
		}
		body, status, err := curlDo(ctx, conn, curlReq{
			Method: "DELETE", URL: "https://api.pagerduty.com/maintenance_windows/" + windowID,
			Headers: headers,
		})
		if err != nil {
			return Fail("pagerduty: " + err.Error()), nil
		}
		if status != 204 {
			return pagerDutyFail("failed to delete the window:", status, body), nil
		}
		return Changed("success").WithExtra("result", decodeJSONOr(body, "")), nil
	}
	return Fail("pagerduty: value of state must be one of: running, started, ongoing, absent, got: " + state), nil
}

// pagerDutyWarnings attaches real's deprecation warning to a result,
// under the Extra["warnings"] key find and cobbler_system already use,
// and only when there is one -- an empty list would make every result
// carry a key real does not send.
//
// Deliberately named for this file rather than shared: go-ansible/modules#111
// introduces a package-wide warning helper, and a second function of
// that name here would be a merge conflict in somebody else's branch.
func pagerDutyWarnings(res Result, warnings []string) Result {
	if len(warnings) == 0 {
		return res
	}
	return res.WithExtra("warnings", warnings)
}

// pagerDutyFail builds the failure Result for a bad status, keeping
// real's sentence and adding what we have in place of fetch_url's
// info['msg']: the status, and the service's own body when it sent one.
func pagerDutyFail(sentence string, status int, body string) Result {
	body = strings.TrimSpace(body)
	if body == "" {
		return Fail(fmt.Sprintf("%s Reason: HTTP %d", sentence, status))
	}
	return Fail(fmt.Sprintf("%s Reason: HTTP %d: %s", sentence, status, body))
}

// decodeJSONOr returns body parsed as JSON, or fallback when it is not
// JSON -- real's own _read_response, which answers "" rather than
// failing a task over a response it could not parse.
func decodeJSONOr(body string, fallback any) any {
	var v any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &v); err != nil {
		return fallback
	}
	return v
}

// jsonOrEmpty encodes v, or returns "" when it cannot be encoded. The
// bodies here are built from strings, maps and slices, so the error is
// unreachable; swallowing it keeps the call sites readable rather than
// threading an error that cannot happen.
func jsonOrEmpty(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// resultFor is Changed("") or Ok("") -- pagerduty_alert's v1 branch
// reports whichever its own comparison found, with no message.
func resultFor(changed bool) Result {
	if changed {
		return Changed("")
	}
	return Ok("")
}

// pagerduty_user is NOT ported, and the reason is its implementation
// rather than its API. It is the one module of the four built on the
// pdpyras SDK: it uses APISession.iter_all("users") to page the whole
// user list, and APISession.persist("users", "email", ...) to upsert by
// email. A faithful port has to reproduce that library's pagination and
// its idempotency rule, not one documented endpoint, so it is a separate
// piece of work rather than a fourth module in this file -- named here
// instead of quietly missing from the registry.
