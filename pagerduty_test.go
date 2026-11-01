package modules

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// pagerDutyBody pulls the JSON document out of the curl config written
// to stdin, so these tests read fields rather than matching substrings
// of a serialisation whose key order is not ours to fix.
func pagerDutyBody(t *testing.T, conn *curlConn, nth int) map[string]any {
	t.Helper()
	const marker = `data = "`
	var bodies []string
	for _, in := range conn.Stdins {
		i := strings.Index(in, marker)
		if i < 0 {
			continue
		}
		rest := in[i+len(marker):]
		j := strings.Index(rest, "\"\n")
		if j < 0 {
			j = len(rest)
		}
		bodies = append(bodies, curlConfigUnescape(rest[:j]))
	}
	if nth >= len(bodies) {
		t.Fatalf("wanted body #%d but only %d request(s) carried one: %q", nth, len(bodies), conn.Stdins)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(bodies[nth]), &out); err != nil {
		t.Fatalf("body #%d is not JSON: %v in %q", nth, err, bodies[nth])
	}
	return out
}

// pagerduty_change's integration_key is a routing key in the BODY, so
// the test that matters is that it travels on stdin and nowhere else.
func TestPagerdutyChangePostsTheEventAndKeepsTheKeyOffArgv(t *testing.T) {
	const key = "PDROUTINGKEY-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: "\nHTTPSTATUS:202"}
	res, err := modulePagerdutyChange(context.Background(), conn, map[string]any{
		"integration_key": key,
		"summary":         "deployed v2",
		"user":            "ci",
		"revision":        "abc123",
		"link_url":        "https://ci.example.com/42",
		"link_text":       "build 42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("changed = false; real reports changed=True on a 202")
	}
	assertSecretsOffArgv(t, conn, map[string]string{"integration_key": key})

	body := pagerDutyBody(t, conn, 0)
	if body["routing_key"] != key {
		t.Errorf("routing_key = %v", body["routing_key"])
	}
	payload, _ := body["payload"].(map[string]any)
	if payload["summary"] != "deployed v2" || payload["source"] != "Ansible" {
		t.Errorf("payload = %v, want real's summary and the default source", payload)
	}
	ts, _ := payload["timestamp"].(string)
	if !strings.HasSuffix(ts, "Z") || len(ts) != len("2006-01-02T15:04:05.000000Z") {
		t.Errorf("timestamp = %q, want real's %%Y-%%m-%%dT%%H:%%M:%%S.%%fZ shape", ts)
	}
	details, _ := payload["custom_details"].(map[string]any)
	if details["user"] != "ci" || details["revision"] != "abc123" {
		t.Errorf("custom_details = %v", details)
	}
	if _, present := details["repo"]; present {
		t.Error("an unset repo reached custom_details; real adds only truthy values")
	}
	links, _ := body["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("links = %v, want the one link real builds", body["links"])
	}
	if l, _ := links[0].(map[string]any); l["href"] != "https://ci.example.com/42" || l["text"] != "build 42" {
		t.Errorf("link = %v", links[0])
	}
}

// Real's dry run POSTs an EMPTY body and treats 400 as the pass, because
// a reachable change-events endpoint rejects an empty event. A 202 is a
// failure there, which is the opposite of the normal path.
func TestPagerdutyChangeCheckModeTreats400AsSuccess(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:400"}
	res, err := modulePagerdutyChange(context.Background(), conn, map[string]any{
		"integration_key": "k", "summary": "s", CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Errorf("res = %+v, want changed and not failed on a 400", res)
	}
	if conn.calls != 1 {
		t.Fatalf("made %d requests in check mode, want the one probe", conn.calls)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if strings.Contains(joined, "data = ") {
		t.Errorf("the probe carried a body; real sends none: %q", joined)
	}

	// And the pass condition really is 400 and not "any answer".
	ok := &curlConn{body: `{"ok":true}` + "\nHTTPSTATUS:202"}
	res2, err := modulePagerdutyChange(context.Background(), ok, map[string]any{
		"integration_key": "k", "summary": "s", CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Failed || !strings.Contains(res2.Msg, "unexpected response: 202") {
		t.Errorf("res = %+v, want real's wording for a non-400 probe", res2)
	}
}

func TestPagerdutyChangeNon202Fails(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:403"}
	res, err := modulePagerdutyChange(context.Background(), conn, map[string]any{
		"integration_key": "k", "summary": "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != "Creating PagerDuty change event failed with 403" {
		t.Errorf("res = %+v, want real's wording", res)
	}
}

// v1 looks before it writes: a GET to the REST API decides whether the
// event POST happens at all. Here the newest incident is already
// triggered, so asking for triggered changes nothing and real sends no
// event.
func TestPagerdutyAlertV1NoChangeSendsNoEvent(t *testing.T) {
	const apiKey = "PDAPIKEY-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: `{"incidents":[{"id":"P1","status":"triggered"}]}` + "\nHTTPSTATUS:200"}
	res, err := modulePagerdutyAlert(context.Background(), conn, map[string]any{
		"state": "triggered", "api_key": apiKey, "service_id": "PSVC1",
		"integration_key": "rk", "name": "db",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Error("changed = true; the newest incident is already triggered")
	}
	if conn.calls != 1 {
		t.Errorf("made %d requests, want only the lookup", conn.calls)
	}
	assertSecretsOffArgv(t, conn, map[string]string{"api_key": apiKey})
	joined := strings.Join(conn.Stdins, "\n")
	for _, want := range []string{
		"Authorization: Token token=" + apiKey,
		"Accept: application/vnd.pagerduty+json;version=2",
		"sort_by=incident_number%3Adesc",
		"service_ids%5B%5D=PSVC1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("lookup missing %q:\n%s", want, joined)
		}
	}
	if res.Extra["result"] == nil {
		t.Error("result is empty; real returns the incident it compared against")
	}
}

// With no incident at all, a trigger WOULD change something and the
// event goes out; acknowledge and resolve would not.
func TestPagerdutyAlertV1NoIncidentDependsOnTheState(t *testing.T) {
	for _, tc := range []struct {
		state   string
		changed bool
		calls   int
	}{
		{"triggered", true, 2},
		{"acknowledged", false, 1},
		{"resolved", false, 1},
	} {
		conn := &curlConn{body: `{"incidents":[]}` + "\nHTTPSTATUS:200"}
		res, err := modulePagerdutyAlert(context.Background(), conn, map[string]any{
			"state": tc.state, "api_key": "k", "service_id": "s",
			"integration_key": "rk", "incident_key": "ik",
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Failed {
			t.Fatalf("%s: res = %+v", tc.state, res)
		}
		if res.Changed != tc.changed {
			t.Errorf("%s: changed = %v, want %v", tc.state, res.Changed, tc.changed)
		}
		if conn.calls != tc.calls {
			t.Errorf("%s: made %d requests, want %d", tc.state, conn.calls, tc.calls)
		}
		if !tc.changed && res.Extra["result"] != "No corresponding incident" {
			t.Errorf("%s: result = %v, want real's own sentence", tc.state, res.Extra["result"])
		}
	}
}

// Real's own typo is part of the contract a user greps for.
func TestPagerdutyAlertV1LookupFailureKeepsRealsWording(t *testing.T) {
	conn := &curlConn{body: "nope\nHTTPSTATUS:401"}
	res, err := modulePagerdutyAlert(context.Background(), conn, map[string]any{
		"state": "triggered", "api_key": "k", "service_id": "s", "integration_key": "rk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a 401 on the lookup did not fail the task")
	}
	if !strings.HasPrefix(res.Msg, "failed to check current incident status. Reason:") {
		t.Errorf("msg = %q, want real's sentence", res.Msg)
	}
}

// v2 has nothing to compare against, so it always reports changed, and
// it DROPS the payload for acknowledge and resolve.
func TestPagerdutyAlertV2DropsThePayloadExceptOnTrigger(t *testing.T) {
	for _, tc := range []struct {
		state          string
		wantAction     string
		wantHasPayload bool
	}{
		{"triggered", "trigger", true},
		{"acknowledged", "acknowledge", false},
		{"resolved", "resolve", false},
	} {
		conn := &curlConn{body: `{"status":"success"}` + "\nHTTPSTATUS:202"}
		res, err := modulePagerdutyAlert(context.Background(), conn, map[string]any{
			"state": tc.state, "api_version": "v2", "integration_key": "rk",
			"source": "db1", "incident_key": "ik", "desc": "disk full",
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Failed {
			t.Fatalf("%s: res = %+v", tc.state, res)
		}
		if !res.Changed {
			t.Errorf("%s: changed = false; v2 always reports changed", tc.state)
		}
		body := pagerDutyBody(t, conn, 0)
		if body["event_action"] != tc.wantAction {
			t.Errorf("%s: event_action = %v, want %q", tc.state, body["event_action"], tc.wantAction)
		}
		if _, has := body["payload"]; has != tc.wantHasPayload {
			t.Errorf("%s: payload present = %v, want %v", tc.state, has, tc.wantHasPayload)
		}
		if body["dedup_key"] != "ik" {
			t.Errorf("%s: dedup_key = %v, want the incident_key", tc.state, body["dedup_key"])
		}
	}
}

// Real refuses a v2 trigger with no source, in a sentence that names
// "service" for an argument called source.
func TestPagerdutyAlertV2TriggerNeedsASource(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:202"}
	res, err := modulePagerdutyAlert(context.Background(), conn, map[string]any{
		"state": "triggered", "api_version": "v2", "integration_key": "rk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed || res.Msg != `"service" is a required variable for v2 api endpoint.` {
		t.Errorf("res = %+v, want real's wording", res)
	}
	if conn.calls != 0 {
		t.Errorf("made %d request(s) for a refused trigger, want 0", conn.calls)
	}
}

// service_key is real's obsolete spelling, and real warns in these exact
// words when it is used.
func TestPagerdutyAlertServiceKeyWarns(t *testing.T) {
	conn := &curlConn{body: `{}` + "\nHTTPSTATUS:202"}
	res, err := modulePagerdutyAlert(context.Background(), conn, map[string]any{
		"state": "triggered", "api_version": "v2", "service_key": "rk", "source": "db1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	warnings, _ := res.Extra["warnings"].([]string)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "obsolete parameter") {
		t.Errorf("warnings = %v, want real's deprecation sentence", res.Extra["warnings"])
	}
	if body := pagerDutyBody(t, conn, 0); body["routing_key"] != "rk" {
		t.Errorf("routing_key = %v; service_key must stand in for integration_key", body["routing_key"])
	}

	// And no warning when the current spelling is used.
	clean := &curlConn{body: `{}` + "\nHTTPSTATUS:202"}
	res2, err := modulePagerdutyAlert(context.Background(), clean, map[string]any{
		"state": "triggered", "api_version": "v2", "integration_key": "rk", "source": "db1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := res2.Extra["warnings"]; present {
		t.Errorf("integration_key warned anyway: %v", res2.Extra["warnings"])
	}
}

// The documented divergence: real sends the v2 event even in check mode.
func TestPagerdutyAlertV2CheckModeSendsNothing(t *testing.T) {
	conn := &curlConn{body: `{}` + "\nHTTPSTATUS:202"}
	res, err := modulePagerdutyAlert(context.Background(), conn, map[string]any{
		"state": "triggered", "api_version": "v2", "integration_key": "rk",
		"source": "db1", CheckModeKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if conn.calls != 0 {
		t.Errorf("made %d request(s) in check mode; a dry run must not page anybody", conn.calls)
	}
	if !SupportsCheckMode("pagerduty_alert") {
		// Not a failure: it documents the state of the gate. The engine
		// skips the task entirely, so this branch is reachable only
		// through Registry.Run.
		t.Log("pagerduty_alert is not in checkModeSupport, so the engine skips it before this branch runs")
	}
}

// Required-argument combinations real enforces in its argument spec,
// before the module body runs.
func TestPagerdutyAlertRequiredCombinations(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"v1 without api_key and service_id",
			map[string]any{"state": "triggered", "integration_key": "rk"},
			"api_version is v1 but all of the following are missing"},
		{"acknowledged without incident_key",
			map[string]any{"state": "acknowledged", "api_version": "v2", "integration_key": "rk"},
			"state is acknowledged but all of the following are missing: incident_key"},
		{"neither key",
			map[string]any{"state": "triggered", "api_version": "v2"},
			"one of the following is required: service_key, integration_key"},
	} {
		conn := &curlConn{body: "\nHTTPSTATUS:202"}
		res, err := modulePagerdutyAlert(context.Background(), conn, tc.args)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Failed || !strings.Contains(res.Msg, tc.want) {
			t.Errorf("%s: res = %+v, want a refusal mentioning %q", tc.name, res, tc.want)
		}
		if conn.calls != 0 {
			t.Errorf("%s: made %d request(s) before validating", tc.name, conn.calls)
		}
	}
}

// pagerduty (the maintenance-window module) sends requester_id as a
// From header, and real refuses to create a window without it.
func TestPagerdutyCreatesAWindow(t *testing.T) {
	const token = "PDTOKEN-aaaaaaaaaaaaaaaa"
	conn := &curlConn{body: `{"maintenance_window":{"id":"W1"}}` + "\nHTTPSTATUS:201"}
	res, err := modulePagerduty(context.Background(), conn, map[string]any{
		"state": "running", "token": token, "service": []any{"PSVC1", "PSVC2"},
		"requester_id": "PUSER1", "hours": "2", "minutes": "30", "desc": "patching",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Msg != "success" {
		t.Fatalf("res = %+v, want real's msg \"success\"", res)
	}
	if !res.Changed {
		t.Error("changed = false; creating a window is a change")
	}
	assertSecretsOffArgv(t, conn, map[string]string{"token": token})
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, "From: PUSER1") {
		t.Errorf("requester_id did not travel as the From header: %q", joined)
	}
	window, _ := pagerDutyBody(t, conn, 0)["maintenance_window"].(map[string]any)
	if window["description"] != "patching" {
		t.Errorf("window = %v", window)
	}
	services, _ := window["services"].([]any)
	if len(services) != 2 {
		t.Fatalf("services = %v, want one reference per service", window["services"])
	}
	if s, _ := services[0].(map[string]any); s["id"] != "PSVC1" || s["type"] != "service_reference" {
		t.Errorf("service reference = %v, want real's {id, type} shape", services[0])
	}
	start, _ := window["start_time"].(string)
	end, _ := window["end_time"].(string)
	if !strings.HasSuffix(start, "Z") || len(start) != len("2006-01-02T15:04:05Z") {
		t.Errorf("start_time = %q, want real's %%Y-%%m-%%dT%%H:%%M:%%SZ shape", start)
	}
	if end <= start {
		t.Errorf("end_time %q is not after start_time %q", end, start)
	}
}

func TestPagerdutyRefusesAWindowWithoutRequesterOrService(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"no service", map[string]any{"state": "started", "token": "t", "requester_id": "u"},
			"service not specified"},
		{"no requester_id", map[string]any{"state": "started", "token": "t", "service": []any{"s"}},
			"requester_id is required when maintenance window should be created"},
	} {
		conn := &curlConn{body: "\nHTTPSTATUS:201"}
		res, err := modulePagerduty(context.Background(), conn, tc.args)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Failed || res.Msg != tc.want {
			t.Errorf("%s: res = %+v, want %q", tc.name, res, tc.want)
		}
	}
}

// ongoing only lists, and reports changed=false.
func TestPagerdutyOngoingOnlyLists(t *testing.T) {
	conn := &curlConn{body: `{"maintenance_windows":[]}` + "\nHTTPSTATUS:200"}
	res, err := modulePagerduty(context.Background(), conn, map[string]any{
		"state": "ongoing", "token": "t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Error("changed = true; a lookup changes nothing")
	}
	if joined := strings.Join(conn.Stdins, "\n"); !strings.Contains(joined, "filter=ongoing") {
		t.Errorf("the ongoing filter is missing: %q", joined)
	}
}

// absent deletes by window_id and real expects a 204.
func TestPagerdutyAbsentDeletesTheWindow(t *testing.T) {
	conn := &curlConn{body: "\nHTTPSTATUS:204"}
	res, err := modulePagerduty(context.Background(), conn, map[string]any{
		"state": "absent", "token": "t", "window_id": "W1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v, want a change", res)
	}
	joined := strings.Join(conn.Stdins, "\n")
	if !strings.Contains(joined, `request = "DELETE"`) {
		t.Errorf("not a DELETE: %q", joined)
	}
	if !strings.Contains(joined, "/maintenance_windows/W1") {
		t.Errorf("window id missing from the url: %q", joined)
	}

	wrong := &curlConn{body: "nope\nHTTPSTATUS:404"}
	res2, err := modulePagerduty(context.Background(), wrong, map[string]any{
		"state": "absent", "token": "t", "window_id": "W1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Failed || !strings.HasPrefix(res2.Msg, "failed to delete the window:") {
		t.Errorf("res = %+v, want real's wording", res2)
	}
}

// The family is the one place a PagerDuty credential could reach argv,
// and all three modules take one. This is the whole point of the curl
// config on stdin, so it gets an assertion that names every one.
func TestPagerdutyFamilyKeepsEveryCredentialOffArgv(t *testing.T) {
	const (
		token  = "PDTOKEN-bbbbbbbbbbbbbbbb"
		apiKey = "PDAPIKEY-bbbbbbbbbbbbbbbb"
		rk     = "PDROUTING-bbbbbbbbbbbbbbbb"
	)
	window := &curlConn{body: `{}` + "\nHTTPSTATUS:201"}
	if _, err := modulePagerduty(context.Background(), window, map[string]any{
		"state": "running", "token": token, "service": []any{"s"}, "requester_id": "u",
	}); err != nil {
		t.Fatal(err)
	}
	assertSecretsOffArgv(t, window, map[string]string{"token": token})

	alert := &curlConn{body: `{"incidents":[]}` + "\nHTTPSTATUS:200"}
	if _, err := modulePagerdutyAlert(context.Background(), alert, map[string]any{
		"state": "triggered", "api_key": apiKey, "service_id": "s", "integration_key": rk,
	}); err != nil {
		t.Fatal(err)
	}
	assertSecretsOffArgv(t, alert, map[string]string{"api_key": apiKey, "integration_key": rk})

	change := &curlConn{body: "\nHTTPSTATUS:202"}
	if _, err := modulePagerdutyChange(context.Background(), change, map[string]any{
		"integration_key": rk, "summary": "s",
	}); err != nil {
		t.Fatal(err)
	}
	assertSecretsOffArgv(t, change, map[string]string{"integration_key": rk})
}
