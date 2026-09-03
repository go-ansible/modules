package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// icinga2CurlRequest issues one curl request against an Icinga2
// server's v1 REST API, shared by icinga2_host.go and
// icinga2_downtime.go — see moduleIcinga2Host's own doc comment for
// why a curl substitution (rather than the `icinga2` CLI
// icinga2_feature.go's own moduleIcinga2Feature shells out to) is used
// here, reusing uri.go's own parseCurlStatus to split the marker-
// tagged response, the same pattern consul_session.go's own
// consulSessionRequest already establishes for an HTTP-API-only
// module.
//
// Args honored: url (the API base, required by both callers);
// url_username/url_password (sent as curl's own `-u user:pass`, i.e.
// HTTP Basic auth — force_basic_auth is accepted but has no effect,
// since curl already sends the Authorization header preemptively with
// `-u`, unlike the httplib2-based real fetch_url this deviates from);
// validate_certs (default true; false adds `-k`); client_cert/
// client_key (`--cert`/`--key`); use_proxy (default true; false adds
// `--noproxy '*'`); http_agent (default "ansible-httpget", sent as
// `-A`); ca_path (`--cacert`, icinga2_downtime.go only — icinga2_host
// has no such arg, so this is simply never set there) and timeout in
// seconds (`--max-time`, icinga2_downtime.go only, default 10 — a
// value of 0 or absent is left as curl's own default). Every request
// also carries an X-HTTP-Method-Override header
// naming method, matching real icinga2_api.call_url /
// Icinga2Client.send_request's own header (sent alongside the actual
// curl -X, not instead of it, since this port's curl invocation needs
// no method-override workaround an HTTP client library might).
func icinga2CurlRequest(ctx context.Context, conn remoteexec.Connection, args map[string]any, method, path, body string) (respBody string, status int, err error) {
	url := strings.TrimRight(argString(args, "url", ""), "/") + "/" + path

	var b strings.Builder
	b.WriteString("curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}") + " -X " + shellQuote(method))
	b.WriteString(" -H " + shellQuote("Accept: application/json"))
	b.WriteString(" -H " + shellQuote("X-HTTP-Method-Override: "+method))
	if agent := argString(args, "http_agent", "ansible-httpget"); agent != "" {
		b.WriteString(" -A " + shellQuote(agent))
	}
	if u := argString(args, "url_username", ""); u != "" {
		p := argString(args, "url_password", "")
		b.WriteString(" -u " + shellQuote(u+":"+p))
	}
	if !argBool(args, "validate_certs", true) {
		b.WriteString(" -k")
	}
	if cert := argString(args, "client_cert", ""); cert != "" {
		b.WriteString(" --cert " + shellQuote(cert))
	}
	if key := argString(args, "client_key", ""); key != "" {
		b.WriteString(" --key " + shellQuote(key))
	}
	if !argBool(args, "use_proxy", true) {
		b.WriteString(" --noproxy " + shellQuote("*"))
	}
	if ca := argString(args, "ca_path", ""); ca != "" {
		b.WriteString(" --cacert " + shellQuote(ca))
	}
	if t := argInt(args, "timeout", 0); t > 0 {
		b.WriteString(" --max-time " + strconv.Itoa(t))
	}
	if body != "" {
		b.WriteString(" -d " + shellQuote(body))
	}
	b.WriteString(" " + shellQuote(url))

	res, err := runStatus(ctx, conn, b.String())
	if err != nil {
		return "", 0, err
	}
	if res.RC != 0 {
		return "", 0, fmt.Errorf("curl failed: %s", strings.TrimSpace(res.Stderr))
	}
	return parseCurlStatus(res.Stdout)
}

// icinga2CurlRequestJSON marshals body to JSON and delegates to
// icinga2CurlRequest.
func icinga2CurlRequestJSON(ctx context.Context, conn remoteexec.Connection, args map[string]any, method, path string, body any) (respBody string, status int, err error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", 0, err
	}
	return icinga2CurlRequest(ctx, conn, args, method, path, string(b))
}

// moduleIcinga2Host implements Ansible's `icinga2_host`
// (community.general) module: adds, updates, or removes a host object
// through the Icinga2 REST API — read from real icinga2_host.py's own
// icinga2_api class (exists/create/modify/delete/diff) (this batch's
// hard rule: the exact request shapes and the attrs-diff idempotency
// check are only visible there, not EXAMPLES/OPTIONS).
//
// Unlike icinga2_feature.go's own moduleIcinga2Feature, there is no
// `icinga2` CLI subcommand for managing a running Icinga2 instance's
// runtime host objects (only for its own local features/config); real
// icinga2_host already speaks the REST API directly via fetch_url, so
// this port's substitution is icinga2CurlRequest's own curl invocation
// (see its own doc comment) rather than an architectural stand-in.
//
// Args: name (required, alias host); url (the Icinga2 API base, e.g.
// "https://icinga2.example.com"); url_username/url_password/
// validate_certs/client_cert/client_key/use_proxy/http_agent/
// force_basic_auth — see icinga2CurlRequest's own doc comment; state
// (present|absent, default present); zone; template (a single name —
// matching real icinga2_host's own `[template]` single-element list
// sent as "templates"); check_command (default hostalive);
// display_name (defaults to name); ip (sent as the host's "address"
// attribute — no longer required, matching real icinga2_host's own
// community.general 8.0.0+ behavior); variables (a dict, each entry
// sent as its own "vars.<key>" attribute, alongside a fixed
// "vars.made_by": "ansible" real icinga2_host always adds).
//
// This module first issues a GET to v1/status; a request-level error
// (curl itself failing, e.g. DNS/connection refused) fails with
// "unable to connect to Icinga. Exception message: ...", matching real
// icinga2_host's own check_connection() call — its own HTTP status
// code is checked internally but the boolean it returns is never
// examined by real icinga2_host.main(), so this port does not check it
// either (a genuine limitation of the upstream module, not a
// simplification introduced here): the module proceeds even against a
// 401/403 status from that probe.
//
// present = a `match("<name>", host.name)` filter query against
// v1/objects/hosts returns exactly one result. state=absent: deletes
// (`DELETE v1/objects/hosts/<name>` with body `{"cascade":1}`) if
// present, otherwise a no-op. state=present, host absent: creates
// (`PUT v1/objects/hosts/<name>`) with the full data (templates +
// attrs). state=present, host present: GETs the object and compares
// each of the module's own attrs against the object's current attrs
// (a key missing there, or an unequal value, counts as changed); if
// anything differs, modifies (`POST v1/objects/hosts/<name>`) with
// attrs ONLY — templates is dropped before a modify request, matching
// real icinga2_host's own `del data["templates"]` (real icinga2_host's
// own doc note: "Template cannot be modified after object creation").
//
// Extra: name, data (the request body actually used for this run's
// operation — templates omitted for a modify, matching real
// icinga2_host's own RETURN VALUES exactly).
func moduleIcinga2Host(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argAliasString(args, "name", "host", "")
	if name == "" {
		return Result{}, errArg("icinga2_host: missing required argument: name")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("icinga2_host: state must be present or absent, got %q", state)
	}
	if argString(args, "url", "") == "" {
		return Result{}, errArg("icinga2_host: missing required argument: url")
	}

	zone := argString(args, "zone", "")
	template := argString(args, "template", "")
	checkCommand := argString(args, "check_command", "hostalive")
	ip := argString(args, "ip", "")
	displayName := argString(args, "display_name", name)
	variables, _ := args["variables"].(map[string]any)

	if _, _, err := icinga2CurlRequest(ctx, conn, args, "GET", "v1/status", ""); err != nil {
		return Fail("unable to connect to Icinga. Exception message: " + err.Error()), nil
	}

	attrs := map[string]any{
		"address":       ip,
		"display_name":  displayName,
		"check_command": checkCommand,
		"zone":          zone,
		"vars.made_by":  "ansible",
	}
	for k, v := range variables {
		attrs["vars."+k] = v
	}
	// Matches real icinga2_host's own `template = []` default (an empty
	// list, not None/null) when no template is given.
	templates := []string{}
	if template != "" {
		templates = []string{template}
	}
	data := map[string]any{"templates": templates, "attrs": attrs}

	exists, err := icinga2HostExists(ctx, conn, args, name)
	if err != nil {
		return Result{}, err
	}

	changed := false
	if exists {
		if state == "absent" {
			respBody, status, err := icinga2CurlRequestJSON(ctx, conn, args, "DELETE", "v1/objects/hosts/"+name, map[string]any{"cascade": 1})
			if err != nil {
				return Result{}, err
			}
			if status != 200 {
				return Fail(fmt.Sprintf("bad return code (%d) deleting host: '%s'", status, respBody)), nil
			}
			changed = true
		} else {
			diffChanged, err := icinga2HostDiff(ctx, conn, args, name, attrs)
			if err != nil {
				return Result{}, err
			}
			if diffChanged {
				data = map[string]any{"attrs": attrs}
				respBody, status, err := icinga2CurlRequestJSON(ctx, conn, args, "POST", "v1/objects/hosts/"+name, data)
				if err != nil {
					return Result{}, err
				}
				if status != 200 {
					return Fail(fmt.Sprintf("bad return code (%d) modifying host: '%s'", status, respBody)), nil
				}
				changed = true
			}
		}
	} else if state == "present" {
		respBody, status, err := icinga2CurlRequestJSON(ctx, conn, args, "PUT", "v1/objects/hosts/"+name, data)
		if err != nil {
			return Result{}, err
		}
		if status != 200 {
			return Fail(fmt.Sprintf("bad return code (%d) creating host: '%s'", status, respBody)), nil
		}
		changed = true
	}

	r := Ok("")
	if changed {
		r = Changed("")
	}
	return r.WithExtra("name", name).WithExtra("data", data), nil
}

func icinga2HostExists(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string) (bool, error) {
	filter := map[string]any{"filter": `match("` + name + `", host.name)`}
	respBody, status, err := icinga2CurlRequestJSON(ctx, conn, args, "GET", "v1/objects/hosts", filter)
	if err != nil {
		return false, err
	}
	if status != 200 {
		return false, nil
	}
	var parsed struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(respBody), &parsed); err != nil {
		return false, fmt.Errorf("icinga2_host: decoding exists response: %w", err)
	}
	return len(parsed.Results) == 1, nil
}

func icinga2HostDiff(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string, attrs map[string]any) (bool, error) {
	respBody, status, err := icinga2CurlRequest(ctx, conn, args, "GET", "v1/objects/hosts/"+name, "")
	if err != nil {
		return false, err
	}
	if status != 200 {
		return false, fmt.Errorf("icinga2_host: unable to read host %s: %s", name, respBody)
	}
	var parsed struct {
		Results []struct {
			Attrs map[string]any `json:"attrs"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(respBody), &parsed); err != nil {
		return false, fmt.Errorf("icinga2_host: decoding host response: %w", err)
	}
	if len(parsed.Results) == 0 {
		return true, nil
	}
	icAttrs := parsed.Results[0].Attrs
	for key, val := range attrs {
		cur, ok := icAttrs[key]
		if !ok || !icinga2ValuesEqual(cur, val) {
			return true, nil
		}
	}
	return false, nil
}

// icinga2ValuesEqual compares two decoded-JSON-or-Ansible-arg values
// loosely (via their string representation), since one side comes
// from this port's own args map (Go string/int/bool) and the other
// from Icinga2's own JSON response (string/float64/bool/nil).
func icinga2ValuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
