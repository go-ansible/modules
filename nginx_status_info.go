package modules

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// nginxStatusExpr matches nginx's stub_status plaintext response body,
// an exact port of real nginx_status_info's own regular expression.
var nginxStatusExpr = regexp.MustCompile(
	`(?s)Active connections: ([0-9]+) \nserver accepts handled requests\n ([0-9]+) ([0-9]+) ([0-9]+) \n` +
		`Reading: ([0-9]+) Writing: ([0-9]+) Waiting: ([0-9]+)`,
)

// moduleNginxStatusInfo implements Ansible's `nginx_status_info`
// (community.general) module: gathers read-only facts from nginx's
// `stub_status` module, whose plaintext status page real
// nginx_status_info fetches with an HTTP GET (via
// ansible.module_utils.urls.fetch_url, run on the CONTROL node against
// url).
//
// Deviation from real nginx_status_info: real nginx_status_info fetches
// url from wherever the module itself runs (the control node, or a
// delegated host) using Ansible's own HTTP client; this port has no
// HTTP client wired into remoteexec.Connection, so it substitutes
// running `curl` ON THE TARGET instead — meaning this port fetches the
// status page as seen FROM the managed node, not from wherever the
// playbook run itself is invoked. For the module's own documented
// default use (url=http://localhost/nginx_status, or another
// loopback/local address on the same host running nginx), this is the
// same observable result; a url pointing at a THIRD host reachable
// from the control node but not from the managed node (or vice versa)
// would behave differently than real nginx_status_info — a real,
// documented gap.
//
// Args: url (string, required — real nginx_status_info's own
// argument_spec has NO default for this; the task brief that prompted
// this port guessed a "http://127.0.0.1/nginx_status"-shaped default,
// but the real module requires the caller to supply it explicitly,
// which is what this port matches); timeout (int, default 10 —
// seconds, passed to `curl --max-time`).
//
// On a fetch failure (curl's own non-zero exit, e.g. connection
// refused or timeout), fails cleanly (Result{Failed:true}), matching
// real nginx_status_info's own `module.fail_json` for "No valid or no
// response from url". On success, parses the response body with
// nginxStatusExpr; a response that does not match (stub_status
// disabled, or a non-nginx server at url) still succeeds with every
// numeric field left unset (nil, surfaced as JSON null) and
// data holding the raw response body — matching real
// nginx_status_info's own behavior exactly (it never fails on a
// non-matching body, only on no response at all).
func moduleNginxStatusInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	url, err := requireString(args, "url")
	if err != nil {
		return Result{}, err
	}
	timeout := argInt(args, "timeout", 10)

	cmd := "curl -s -S --max-time " + strconv.Itoa(timeout) + " " + shellQuote(url)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	result := map[string]any{
		"active_connections": nil,
		"accepts":            nil,
		"handled":            nil,
		"requests":           nil,
		"reading":            nil,
		"writing":            nil,
		"waiting":            nil,
		"data":               nil,
	}
	if res.RC != 0 {
		return Fail("nginx_status_info: no valid or no response from url " + url + " within " +
			strconv.Itoa(timeout) + " seconds (timeout): " + strings.TrimSpace(res.Stderr)), nil
	}

	data := res.Stdout
	if data == "" {
		return okResultFromMap(result), nil
	}
	result["data"] = data

	if m := nginxStatusExpr.FindStringSubmatch(data); m != nil {
		result["active_connections"] = mustAtoi(m[1])
		result["accepts"] = mustAtoi(m[2])
		result["handled"] = mustAtoi(m[3])
		result["requests"] = mustAtoi(m[4])
		result["reading"] = mustAtoi(m[5])
		result["writing"] = mustAtoi(m[6])
		result["waiting"] = mustAtoi(m[7])
	}
	return okResultFromMap(result), nil
}

func okResultFromMap(m map[string]any) Result {
	r := Ok("")
	for k, v := range m {
		r = r.WithExtra(k, v)
	}
	return r
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
