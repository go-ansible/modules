package modules

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const (
	gandiPAT    = "GANDIPAT-0123456789abcdef"
	gandiAPIKey = "GANDIKEY-fedcba9876543210"
)

// gandiHTTP wraps a body in the trailer curlDo's own parser reads, so a
// test says what the API answered rather than how curl reported it.
func gandiHTTP(status int, body string) string {
	return body + "\nHTTPSTATUS:" + strconv.Itoa(status)
}

// gandiReq is one request as it reached curl's stdin config. Asserting
// on this rather than on argv is the point: for this module the
// credential is a header, and a header on a command line would be
// readable in the target's process list.
type gandiReq struct {
	method  string
	url     string
	data    string
	headers []string
}

func gandiReqs(t *testing.T, conn *curlConn) []gandiReq {
	t.Helper()
	out := make([]gandiReq, 0, len(conn.Stdins))
	for _, cfg := range conn.Stdins {
		var r gandiReq
		for _, line := range strings.Split(cfg, "\n") {
			switch {
			case strings.HasPrefix(line, `url = "`):
				r.url = curlConfigUnescape(strings.TrimSuffix(strings.TrimPrefix(line, `url = "`), `"`))
			case strings.HasPrefix(line, `request = "`):
				r.method = curlConfigUnescape(strings.TrimSuffix(strings.TrimPrefix(line, `request = "`), `"`))
			case strings.HasPrefix(line, `data = "`):
				r.data = curlConfigUnescape(strings.TrimSuffix(strings.TrimPrefix(line, `data = "`), `"`))
			case strings.HasPrefix(line, `header = "`):
				r.headers = append(r.headers,
					curlConfigUnescape(strings.TrimSuffix(strings.TrimPrefix(line, `header = "`), `"`)))
			}
		}
		out = append(out, r)
	}
	return out
}

func gandiArgs() map[string]any {
	return map[string]any{
		"personal_access_token": gandiPAT,
		"domain":                "my.com",
		"record":                "test",
		"type":                  "A",
	}
}

// gandiRecord is the API's own shape for one record set.
func gandiRecord(name, rrType string, ttl int, values ...string) string {
	v, _ := json.Marshal(values)
	return `{"rrset_name": "` + name + `", "rrset_type": "` + rrType +
		`", "rrset_ttl": ` + strconv.Itoa(ttl) + `, "rrset_values": ` + string(v) + `}`
}

func gandiBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("request body is not JSON: %q (%v)", raw, err)
	}
	return m
}

func TestGandiLivednsCreatesWhenAbsent(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(404, `{"errors": [{"name": "record", "description": "not found"}]}`),
		gandiHTTP(201, `{"message": "DNS Record Created"}`),
	}}
	args := gandiArgs()
	args["ttl"] = 7200
	args["values"] = []any{"127.0.0.1"}

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("failed: %s", res.Msg)
	}
	if !res.Changed {
		t.Error("creating a record set is a change")
	}

	reqs := gandiReqs(t, conn)
	if len(reqs) != 2 {
		t.Fatalf("want a lookup then a create, got %d request(s)", len(reqs))
	}
	if reqs[0].method != "GET" || reqs[0].url != gandiLiveDNSEndpoint+"/domains/my.com/records/test/A" {
		t.Errorf("lookup = %s %s", reqs[0].method, reqs[0].url)
	}
	if reqs[1].method != "POST" || reqs[1].url != gandiLiveDNSEndpoint+"/domains/my.com/records" {
		t.Errorf("create = %s %s", reqs[1].method, reqs[1].url)
	}
	body := gandiBody(t, reqs[1].data)
	if body["rrset_name"] != "test" || body["rrset_type"] != "A" {
		t.Errorf("create body names %v/%v", body["rrset_name"], body["rrset_type"])
	}
	if body["rrset_ttl"] != float64(7200) {
		t.Errorf("rrset_ttl = %v, want 7200", body["rrset_ttl"])
	}
	if got := body["rrset_values"]; !reflect.DeepEqual(got, []any{"127.0.0.1"}) {
		t.Errorf("rrset_values = %v", got)
	}

	// Real returns the record it SENT, so the reported set is complete
	// even though the API's own answer carried only a message.
	rec, _ := res.Extra["record"].(map[string]any)
	want := map[string]any{
		"record": "test", "type": "A", "ttl": 7200,
		"values": []string{"127.0.0.1"}, "domain": "my.com",
	}
	if !reflect.DeepEqual(rec, want) {
		t.Errorf("record = %#v\nwant %#v", rec, want)
	}

	assertSecretsOffArgv(t, conn, map[string]string{"personal_access_token": gandiPAT})
}

// A DNS record set is unordered, and real compares values as SETS. A
// task that lists the same addresses in another order has changed
// nothing, and reporting a change there would make every run of such a
// playbook report work it did not do.
func TestGandiLivednsIsUnchangedWhenValuesMatchInAnyOrder(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.91", "192.0.2.92")),
	}}
	args := gandiArgs()
	args["ttl"] = 300
	args["values"] = []any{"192.0.2.92", "192.0.2.91"}

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("the same values in another order is not a change")
	}
	if n := len(gandiReqs(t, conn)); n != 1 {
		t.Errorf("want the lookup alone, got %d request(s)", n)
	}
}

func TestGandiLivednsUpdatesWhenTTLDiffers(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.91")),
		gandiHTTP(200, `{"message": "DNS Record Updated"}`),
		gandiHTTP(200, gandiRecord("test", "A", 10800, "192.0.2.91")),
	}}
	args := gandiArgs()
	args["ttl"] = 10800
	args["values"] = []any{"192.0.2.91"}

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("a new TTL is a change")
	}

	reqs := gandiReqs(t, conn)
	if len(reqs) != 3 {
		t.Fatalf("want lookup, update, re-read; got %d", len(reqs))
	}
	if reqs[1].method != "PUT" || reqs[1].url != gandiLiveDNSEndpoint+"/domains/my.com/records/test/A" {
		t.Errorf("update = %s %s", reqs[1].method, reqs[1].url)
	}
	body := gandiBody(t, reqs[1].data)
	if body["rrset_ttl"] != float64(10800) {
		t.Errorf("update ttl = %v", body["rrset_ttl"])
	}
	// Real re-reads rather than trusting the PUT's answer, so the TTL
	// reported is the zone's own.
	rec, _ := res.Extra["record"].(map[string]any)
	if rec["ttl"] != 10800 {
		t.Errorf("reported ttl = %v, want the re-read 10800", rec["ttl"])
	}
}

func TestGandiLivednsCheckModeCreatesNothing(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(404, `{}`),
	}}
	args := gandiArgs()
	args["ttl"] = 7200
	args["values"] = []any{"127.0.0.1"}
	args["_ansible_check_mode"] = true

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("a dry run still reports the change it would make")
	}
	reqs := gandiReqs(t, conn)
	if len(reqs) != 1 {
		t.Fatalf("a dry run must send the lookup only, got %d request(s)", len(reqs))
	}
	if reqs[0].method != "GET" {
		t.Errorf("the one request was %s, not a read", reqs[0].method)
	}
	rec, _ := res.Extra["record"].(map[string]any)
	if rec["ttl"] != 7200 || rec["record"] != "test" {
		t.Errorf("a dry run reports what it would write, got %#v", rec)
	}
}

func TestGandiLivednsAbsentDeletesTheRecordSet(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.91")),
		gandiHTTP(204, ``),
	}}
	args := gandiArgs()
	args["state"] = "absent"

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("removing a record set is a change")
	}
	if _, ok := res.Extra["record"]; ok {
		t.Error("a deletion reports no record, which is real's own 'except on record deletion'")
	}
	reqs := gandiReqs(t, conn)
	if len(reqs) != 2 || reqs[1].method != "DELETE" {
		t.Fatalf("want a lookup then a DELETE, got %d request(s) ending %q", len(reqs), reqs[len(reqs)-1].method)
	}
}

// state=absent with `values` SUBTRACTS them. When some of the current
// values survive, the set is updated to the remainder rather than
// deleted -- an asymmetry real has and its documentation does not
// mention.
func TestGandiLivednsAbsentSubtractsSomeValuesAndKeepsTheRest(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.93", "192.0.2.91", "192.0.2.92")),
		gandiHTTP(200, `{"message": "DNS Record Updated"}`),
		gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.91", "192.0.2.93")),
	}}
	args := gandiArgs()
	args["state"] = "absent"
	args["values"] = []any{"192.0.2.92"}

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("dropping one value is a change")
	}

	reqs := gandiReqs(t, conn)
	if len(reqs) != 3 {
		t.Fatalf("want lookup, update, re-read; got %d", len(reqs))
	}
	if reqs[1].method != "PUT" {
		t.Fatalf("the write was %s, want a PUT of the remainder", reqs[1].method)
	}
	body := gandiBody(t, reqs[1].data)
	// Sorted: real sends a Python set, whose order differs run to run
	// for the same inputs. See the module's stated divergence.
	if got := body["rrset_values"]; !reflect.DeepEqual(got, []any{"192.0.2.91", "192.0.2.93"}) {
		t.Errorf("remainder = %v, want the surviving values sorted", got)
	}
	// The deleted one must not come back in the request.
	if strings.Contains(reqs[1].data, "192.0.2.92") {
		t.Errorf("the subtracted value is still in the body: %s", reqs[1].data)
	}
}

// ⛔ The divergence this module exists to declare. Real's
// delete_dns_record reaches update_record on this branch with NO
// check_mode guard, so `--check` against real EDITS a live zone. A dry
// run here sends the lookup and nothing else.
//
// Neuter: drop the `if checkMode` return in gandiAPI.remove and this
// fails with "a dry run sent 2 request(s)", naming the PUT.
func TestGandiLivednsCheckModeSendsNoWriteWhenSubtracting(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.91", "192.0.2.92")),
	}}
	args := gandiArgs()
	args["state"] = "absent"
	args["values"] = []any{"192.0.2.92"}
	args["_ansible_check_mode"] = true

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("a dry run still reports the change it would make")
	}

	reqs := gandiReqs(t, conn)
	if len(reqs) != 1 {
		for _, r := range reqs[1:] {
			t.Errorf("a dry run sent %s %s", r.method, r.url)
		}
		t.Fatalf("a dry run sent %d request(s); only the lookup may be sent", len(reqs))
	}
	for _, r := range reqs {
		if r.method != "GET" {
			t.Errorf("a dry run sent a %s", r.method)
		}
	}
	// It still says what it would have left behind.
	rec, _ := res.Extra["record"].(map[string]any)
	if got, ok := rec["values"].([]string); !ok || !reflect.DeepEqual(got, []string{"192.0.2.91"}) {
		t.Errorf("reported remainder = %#v", rec["values"])
	}
}

// The apex is "" in a playbook and "@" to the API, in both directions.
func TestGandiLivednsApexIsSentAsAtAndReportedEmpty(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(200, gandiRecord("@", "A", 300, "192.0.2.91")),
	}}
	args := gandiArgs()
	args["record"] = ""
	args["ttl"] = 300
	args["values"] = []any{"192.0.2.91"}

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	reqs := gandiReqs(t, conn)
	if len(reqs) != 1 || !strings.HasSuffix(reqs[0].url, "/records/@/A") {
		t.Fatalf("apex lookup url = %q", reqs[0].url)
	}
	rec, _ := res.Extra["record"].(map[string]any)
	if rec["record"] != "" {
		t.Errorf("the apex is reported as %q, want the empty name a playbook wrote", rec["record"])
	}
}

func TestGandiLivednsSendsBearerForATokenAndApikeyForAKey(t *testing.T) {
	for _, tc := range []struct {
		name, arg, secret, want string
	}{
		{"personal access token", "personal_access_token", gandiPAT, "Authorization: Bearer " + gandiPAT},
		{"api key", "api_key", gandiAPIKey, "Authorization: Apikey " + gandiAPIKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &curlConn{bodies: []string{
				gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.91")),
			}}
			args := map[string]any{
				"domain": "my.com", "record": "test", "type": "A",
				"ttl": 300, "values": []any{"192.0.2.91"},
				tc.arg: tc.secret,
			}
			if _, err := moduleGandiLivedns(context.Background(), conn, args); err != nil {
				t.Fatal(err)
			}
			reqs := gandiReqs(t, conn)
			if len(reqs) == 0 {
				t.Fatal("no request was sent")
			}
			var found bool
			for _, h := range reqs[0].headers {
				if h == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("headers = %q, want %q", reqs[0].headers, tc.want)
			}
			assertSecretsOffArgv(t, conn, map[string]string{tc.arg: tc.secret})
		})
	}
}

func TestGandiLivednsArgumentValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "both credentials",
			args: map[string]any{
				"domain": "my.com", "record": "test", "type": "A", "ttl": 300,
				"values":  []any{"192.0.2.91"},
				"api_key": gandiAPIKey, "personal_access_token": gandiPAT,
			},
			want: "parameters are mutually exclusive: api_key|personal_access_token",
		},
		{
			name: "no credential",
			args: map[string]any{
				"domain": "my.com", "record": "test", "type": "A", "ttl": 300,
				"values": []any{"192.0.2.91"},
			},
			want: "one of the following is required: api_key, personal_access_token",
		},
		{
			name: "present without values or ttl",
			args: map[string]any{
				"personal_access_token": gandiPAT,
				"domain":                "my.com", "record": "test", "type": "A",
			},
			want: "state is present but all of the following are missing: values, ttl",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := &curlConn{bodies: []string{gandiHTTP(200, `{}`)}}
			res, err := moduleGandiLivedns(context.Background(), conn, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Failed {
				t.Fatalf("want a refusal, got changed=%v", res.Changed)
			}
			if res.Msg != tc.want {
				t.Errorf("msg = %q\nwant %q", res.Msg, tc.want)
			}
			if n := len(gandiReqs(t, conn)); n != 0 {
				t.Errorf("a refused task sent %d request(s); validation precedes the API", n)
			}
		})
	}
}

// A 404 on the lookup is an ANSWER -- "no such record set" -- not a
// failure. state=absent on a record that is already gone reports no
// change, and real's error_on_404=False is what makes that possible.
func TestGandiLivednsAbsentOnAMissingRecordIsNoChange(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(404, `{"errors": [{"name": "record", "description": "not found"}]}`),
	}}
	args := gandiArgs()
	args["state"] = "absent"

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("a 404 on the lookup is not a failure: %s", res.Msg)
	}
	if res.Changed {
		t.Error("nothing was there to remove")
	}
	if n := len(gandiReqs(t, conn)); n != 1 {
		t.Errorf("want the lookup alone, got %d", n)
	}
}

// A real error still fails, with the API's own words. This is the
// control for the test above: error_on_404=False must not swallow
// everything.
func TestGandiLivednsReportsTheApiError(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(401, `{"errors": [{"name": "authorization", "description": "Invalid credentials"}]}`),
	}}
	args := gandiArgs()
	args["ttl"] = 300
	args["values"] = []any{"192.0.2.91"}

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("a 401 must fail the task")
	}
	if want := "API Error Permission denied: authorization :Invalid credentials"; res.Msg != want {
		t.Errorf("msg = %q\nwant %q", res.Msg, want)
	}
}

func TestGandiLivednsDiffShowsBothSides(t *testing.T) {
	conn := &curlConn{bodies: []string{
		gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.91")),
		gandiHTTP(200, `{"message": "DNS Record Updated"}`),
		gandiHTTP(200, gandiRecord("test", "A", 300, "192.0.2.92")),
	}}
	args := gandiArgs()
	args["ttl"] = 300
	args["values"] = []any{"192.0.2.92"}
	args["_ansible_diff"] = true

	res, err := moduleGandiLivedns(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diffs) != 1 {
		t.Fatalf("want one diff, got %d", len(res.Diffs))
	}
	d := res.Diffs[0]
	if !strings.Contains(d.Before, "192.0.2.91") {
		t.Errorf("before = %q", d.Before)
	}
	if !strings.Contains(d.After, "192.0.2.92") {
		t.Errorf("after = %q", d.After)
	}
}

// A module that is not registered is unreachable from a playbook, which
// no test of the function itself would notice.
func TestGandiLivednsIsRegisteredAndSupportsCheckMode(t *testing.T) {
	// Default(), not NewRegistry(): the latter is EMPTY, so asserting
	// against it would fail for a correctly wired module and pass for
	// nothing at all.
	if _, ok := Default().Get("gandi_livedns"); !ok {
		t.Error("gandi_livedns is not in the registry")
	}
	if _, ok := Default().Get("community.general.gandi_livedns"); !ok {
		t.Error("the fully-qualified name does not resolve")
	}
	if !SupportsCheckMode("gandi_livedns") {
		t.Error("gandi_livedns guards every write and should declare check-mode support")
	}
}
