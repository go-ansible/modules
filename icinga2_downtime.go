package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIcinga2Downtime implements Ansible's `icinga2_downtime`
// (community.general) module: schedules or removes an Icinga2
// downtime through the Icinga2 REST API's own v1/actions/
// schedule-downtime and v1/actions/remove-downtime endpoints — read
// from real icinga2_downtime.py's own Icinga2Downtime.state_present/
// state_absent and module_utils/_icinga2.py's own Actions.
// schedule_downtime/remove_downtime (this batch's hard rule: the exact
// request body shape and the 2xx/404/4xx status-code branching are
// only visible there, not EXAMPLES/OPTIONS).
//
// There is no `icinga2` CLI subcommand for scheduling ad hoc runtime
// downtimes (unlike icinga2_feature.go's own local-config-file
// `icinga2 feature enable/disable`); real icinga2_downtime already
// speaks the REST API directly, so this port's substitution is
// icinga2CurlRequest's own curl invocation (icinga2_host.go's own doc
// comment), not an architectural stand-in.
//
// Args: url (required); url_username/url_password/validate_certs/
// client_cert/client_key/use_proxy/http_agent/force_basic_auth/
// ca_path/timeout (default 10, seconds) — see icinga2CurlRequest's own
// doc comment; state (present|absent, default present); object_type
// (Service|Host|Downtime, default Host); filter, name — at least one
// of the two is required (matching real icinga2_downtime's own
// required_one_of), independent of state; filter_vars (dict);
// author (default "Ansible"); comment (default "Downtime scheduled by
// Ansible"); start_time, end_time (both required, UNIX timestamps,
// state=present only) — end_time must be later than start_time,
// otherwise Fail("The end time must be later than the start time.");
// duration — required if fixed is explicitly set to false (matching
// real icinga2_downtime's own required_if), otherwise defaults to
// end_time-start_time when omitted; fixed (bool, tri-state: unset vs
// explicit true/false, since Icinga2 itself only omits the "fixed"
// field from the request — defaulting to a fixed downtime server-side
// — when the module omits it too); all_services (bool, same tri-state
// reasoning); trigger_name; child_options
// (DowntimeNoChildren|DowntimeTriggeredChildren|
// DowntimeNonTriggeredChildren).
//
// state=present: POSTs v1/actions/schedule-downtime with
// {type, filter, author, comment, start_time, end_time, duration,
// [filter_vars, fixed, all_services, trigger_name, child_options]}.
// state=absent: POSTs v1/actions/remove-downtime with
// {type, [<type-lower-cased>: name], [filter], [filter_vars]} — real
// remove_downtime's own key name for name is object_type.lower()
// (e.g. "host" for object_type=Host, "downtime" for
// object_type=Downtime), reproduced exactly.
//
// A 2xx response is Changed, with Extra["results"] set from the
// response body's own "results" array. For state=absent specifically,
// a 404 is treated as Ok (unchanged, "No matching downtime object
// found."), not a failure — matching real icinga2_downtime's own
// special-cased status_code==404 branch. Any other 4xx+ response
// fails, with Extra["error"] set to the response body's own decoded
// JSON if it parses (matching real icinga2_downtime's own
// `with suppress(...): self.vars.set("error", ...)`).
//
// Deviation: real icinga2_downtime declares check_mode support: none
// (an intentionally unsupported combination, per its own ATTRIBUTES
// documentation, because a complex filter expression makes success
// prediction unreliable); this port does not implement check_mode at
// all across the whole package (see ufw.go's own doc comment on this
// port's own general check_mode stance), so this is not a narrowing
// specific to this module.
func moduleIcinga2Downtime(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if argString(args, "url", "") == "" {
		return Result{}, errArg("icinga2_downtime: missing required argument: url")
	}
	filter := argString(args, "filter", "")
	name := argString(args, "name", "")
	if filter == "" && name == "" {
		return Result{}, errArg("icinga2_downtime: one of filter or name is required")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("icinga2_downtime: state must be present or absent, got %q", state)
	}
	objectType := argString(args, "object_type", "Host")
	switch objectType {
	case "Service", "Host", "Downtime":
	default:
		return Result{}, errArg("icinga2_downtime: object_type must be one of Service, Host, Downtime, got %q", objectType)
	}

	// Real icinga2_argument_spec() declares timeout with default=10,
	// unlike icinga2_host's own url_argument_spec() (which has no
	// timeout at all — see icinga2CurlRequest's own doc comment); inject
	// that default here, on a shallow copy, rather than mutating the
	// caller's own args map.
	if _, ok := args["timeout"]; !ok {
		withTimeout := make(map[string]any, len(args)+1)
		for k, v := range args {
			withTimeout[k] = v
		}
		withTimeout["timeout"] = 10
		args = withTimeout
	}

	if state == "present" {
		return icinga2DowntimePresent(ctx, conn, args, objectType, filter)
	}
	return icinga2DowntimeAbsent(ctx, conn, args, objectType, name, filter)
}

func icinga2DowntimePresent(ctx context.Context, conn remoteexec.Connection, args map[string]any, objectType, filter string) (Result, error) {
	if filter == "" {
		return Result{}, errArg("icinga2_downtime: filter is required when state=present")
	}
	if _, ok := args["start_time"]; !ok {
		return Result{}, errArg("icinga2_downtime: start_time is required when state=present")
	}
	if _, ok := args["end_time"]; !ok {
		return Result{}, errArg("icinga2_downtime: end_time is required when state=present")
	}
	startTime := argInt(args, "start_time", 0)
	endTime := argInt(args, "end_time", 0)
	if endTime <= startTime {
		return Fail("The end time must be later than the start time."), nil
	}
	author := argString(args, "author", "Ansible")
	comment := argString(args, "comment", "Downtime scheduled by Ansible")

	var fixedPtr *bool
	if _, ok := args["fixed"]; ok {
		b := argBool(args, "fixed", true)
		fixedPtr = &b
	}
	var duration int
	if _, ok := args["duration"]; ok {
		duration = argInt(args, "duration", 0)
	} else {
		if fixedPtr != nil && !*fixedPtr {
			return Result{}, errArg("icinga2_downtime: duration is required when fixed=false")
		}
		duration = endTime - startTime
	}

	body := map[string]any{
		"type": objectType, "filter": filter, "author": author, "comment": comment,
		"start_time": startTime, "end_time": endTime, "duration": duration,
	}
	if fv, ok := args["filter_vars"].(map[string]any); ok {
		body["filter_vars"] = fv
	}
	if fixedPtr != nil {
		body["fixed"] = *fixedPtr
	}
	if _, ok := args["all_services"]; ok {
		body["all_services"] = argBool(args, "all_services", false)
	}
	if tn := argString(args, "trigger_name", ""); tn != "" {
		body["trigger_name"] = tn
	}
	if co := argString(args, "child_options", ""); co != "" {
		body["child_options"] = co
	}

	respBody, status, err := icinga2CurlRequestJSON(ctx, conn, args, "POST", "v1/actions/schedule-downtime", body)
	if err != nil {
		return Result{}, err
	}

	if status >= 200 && status <= 299 {
		results, err := icinga2ParseResults(respBody)
		if err != nil {
			return Result{}, err
		}
		return Changed("Successfully scheduled downtime.").WithExtra("results", results), nil
	}
	if status >= 400 {
		r := Fail("Unable to schedule downtime.")
		if errObj, ok := icinga2ParseError(respBody); ok {
			r = r.WithExtra("error", errObj)
		}
		return r, nil
	}
	return Ok(""), nil
}

func icinga2DowntimeAbsent(ctx context.Context, conn remoteexec.Connection, args map[string]any, objectType, name, filter string) (Result, error) {
	body := map[string]any{"type": objectType}
	if name != "" {
		body[strings.ToLower(objectType)] = name
	}
	if filter != "" {
		body["filter"] = filter
	}
	if fv, ok := args["filter_vars"].(map[string]any); ok {
		body["filter_vars"] = fv
	}

	respBody, status, err := icinga2CurlRequestJSON(ctx, conn, args, "POST", "v1/actions/remove-downtime", body)
	if err != nil {
		return Result{}, err
	}

	if status >= 200 && status <= 299 {
		results, err := icinga2ParseResults(respBody)
		if err != nil {
			return Result{}, err
		}
		return Changed("Successfully removed downtime.").WithExtra("results", results), nil
	}
	if status == 404 {
		return Ok("No matching downtime object found."), nil
	}
	if status >= 400 {
		r := Fail("Unable to remove downtime.")
		if errObj, ok := icinga2ParseError(respBody); ok {
			r = r.WithExtra("error", errObj)
		}
		return r, nil
	}
	return Ok(""), nil
}

func icinga2ParseResults(body string) ([]map[string]any, error) {
	var parsed struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, errArg("icinga2_downtime: decoding response %q: %v", body, err)
	}
	return parsed.Results, nil
}

func icinga2ParseError(body string) (map[string]any, bool) {
	var errObj map[string]any
	if err := json.Unmarshal([]byte(body), &errObj); err != nil {
		return nil, false
	}
	return errObj, true
}
