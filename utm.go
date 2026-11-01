package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// Sophos UTM (community.general's utm_* family) is thirteen modules over
// ONE algorithm, which real implements once in
// module_utils/_utm_utils.py. That algorithm is here, and each module is
// a utmSpec naming its endpoint, the keys that decide whether an
// existing object has changed, and its argument defaults.
//
// Eight of the thirteen are registered: all five info modules, and the
// three write modules whose argument specs are small enough to have been
// read in full and transcribed exactly (utm_dns_host,
// utm_network_interface_address, utm_ca_host_key_cert). The other five --
// utm_aaa_group and the four reverse_proxy write modules -- declare
// between fourteen and twenty-five fields each, and a default
// transcribed wrong is a silently different request to a firewall, so
// they are left for their own change rather than guessed at here. The
// engine below is already theirs; only the specs are missing.
//
// Everything goes through curl ON THE TARGET, with the url, the headers
// and the body on curl's stdin config.
//
// # The credential is also in the body, because real puts it there
//
// Real authenticates with HTTP Basic as user "token", password
// utm_token -- and then sends `module.params` as the request body, which
// still contains utm_token, url_username and url_password. So the token
// reaches the device twice: once in the Authorization header and once in
// the JSON. That is reproduced rather than tidied, for two reasons: the
// body is what the device's API actually receives from real, and
// `_clean_result` exists in real precisely because real knows it sent
// those fields and strips them back out of the response. Deviating would
// change the request a device sees, and no device is available to
// measure the difference against.
//
// Nothing reaches argv either way, which is the rule that matters here.
type utmSpec struct {
	endpoint   string     // appended to /api/objects/<endpoint>/
	changeKeys []string   // real's key_to_check_for_changes
	infoOnly   bool       // real's info_only: GET, never write
	fields     []utmField // the module's own arguments, in real's order
}

// utmField is one declared argument: its name, whether real requires it,
// and the default real applies when it is absent. A nil def means real
// declares no default, and the key is then absent from the body rather
// than present-and-empty -- which is the difference between "leave this
// alone" and "set this to nothing" on a UTM object.
type utmField struct {
	name     string
	required bool
	def      any
}

// utmConnectionFields are the arguments real's UTMModule adds to every
// module in the family.
var utmConnectionFields = []utmField{
	{name: "utm_host", required: true},
	{name: "utm_port", def: 4444},
	{name: "utm_token", required: true},
	{name: "utm_protocol", def: "https"},
	{name: "validate_certs", def: true},
	{name: "state", def: "present"},
}

// runUTM is real's UTM.execute: look the object up by name, then add,
// update, remove or just report it.
func runUTM(ctx context.Context, conn remoteexec.Connection, moduleName string, spec utmSpec, args map[string]any) (Result, error) {
	host, err := requireString(args, "utm_host")
	if err != nil {
		return Result{}, err
	}
	token, err := requireString(args, "utm_token")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	protocol := argString(args, "utm_protocol", "https")
	if protocol != "https" && protocol != "http" {
		return Fail(moduleName + ": value of utm_protocol must be one of: https, http, got: " + protocol), nil
	}
	state := argString(args, "state", "present")
	if !spec.infoOnly && state != "present" && state != "absent" {
		return Fail(moduleName + ": value of state must be one of: present, absent, got: " + state), nil
	}
	for _, f := range spec.fields {
		if f.required {
			if _, err := requireString(args, f.name); err != nil {
				return Result{}, err
			}
		}
	}

	base := fmt.Sprintf("%s://%s:%d/api/objects/%s/",
		protocol, host, argInt(args, "utm_port", 4444), spec.endpoint)
	// Basic auth as user "token": on curl's stdin, never -u.
	auth := "token:" + token

	if res, ok := requireCurl(ctx, conn, moduleName, "Sophos UTM"); !ok {
		return res, nil
	}

	found, res, err := utmLookup(ctx, conn, moduleName, base, auth, name)
	if err != nil || res.Failed {
		return res, err
	}

	if spec.infoOnly {
		// Real exits changed=False either way, and omits the result key
		// entirely when there is no such object.
		if found == nil {
			return Ok(""), nil
		}
		return Ok("").WithExtra("result", found), nil
	}

	body := utmBody(spec, args, token)

	if state == "absent" {
		if found == nil {
			// Real reports changed=False and no result at all.
			return Ok(""), nil
		}
		ref, _ := found["_ref"].(string)
		_, status, err := curlDo(ctx, conn, curlReq{
			Method: "DELETE", URL: base + ref, BasicAuth: auth, JSON: body,
			// Real's own two headers for the delete, including the
			// acknowledgement the device demands before it will remove
			// an object other objects may reference.
			Headers: [][2]string{
				{"Accept", "application/json"},
				{"X-Restd-Err-Ack", "all"},
			},
		})
		if err != nil {
			return Fail(moduleName + ": " + err.Error()), nil
		}
		if status >= 400 {
			return Fail(fmt.Sprintf("%s: removing %s: HTTP %d", moduleName, name, status)), nil
		}
		return Changed(""), nil
	}

	headers := utmHeaders(args)
	if found == nil {
		respBody, status, err := curlDo(ctx, conn, curlReq{
			Method: "POST", URL: base, BasicAuth: auth, JSON: body, Headers: headers,
		})
		if err != nil {
			return Fail(moduleName + ": " + err.Error()), nil
		}
		if status >= 400 {
			return Fail(fmt.Sprintf("%s: creating %s: HTTP %d: %s",
				moduleName, name, status, strings.TrimSpace(respBody))), nil
		}
		return Changed("").WithExtra("result", utmCleanResult(respBody)), nil
	}

	changed, missing := utmObjectChanged(spec.changeKeys, args, found)
	if missing != "" {
		// Real does `result[key]` and the KeyError lands in execute()'s
		// `except Exception` as a bare message. Saying which key is
		// missing is the same failure with the useful half kept.
		return Fail(fmt.Sprintf("%s: the device's %s object has no %q field to compare against",
			moduleName, spec.endpoint, missing)), nil
	}
	if !changed {
		return Ok("").WithExtra("result", found), nil
	}
	ref, _ := found["_ref"].(string)
	respBody, status, err := curlDo(ctx, conn, curlReq{
		Method: "PUT", URL: base + ref, BasicAuth: auth, JSON: body, Headers: headers,
	})
	if err != nil {
		return Fail(moduleName + ": " + err.Error()), nil
	}
	if status >= 400 {
		return Fail(fmt.Sprintf("%s: updating %s: HTTP %d: %s",
			moduleName, name, status, strings.TrimSpace(respBody))), nil
	}
	return Changed("").WithExtra("result", utmCleanResult(respBody)), nil
}

// utmLookup is real's _lookup_entry: GET the whole collection and pick
// the element whose "name" matches. The device has no by-name endpoint,
// which is why every operation here costs a list first.
func utmLookup(ctx context.Context, conn remoteexec.Connection, moduleName, base, auth, name string) (map[string]any, Result, error) {
	body, status, err := curlDo(ctx, conn, curlReq{
		Method: "GET", URL: base, BasicAuth: auth,
		Headers: [][2]string{{"Accept", "application/json"}},
	})
	if err != nil {
		return nil, Fail(moduleName + ": " + err.Error()), nil
	}
	if status >= 400 {
		return nil, Fail(fmt.Sprintf("%s: looking up %s: HTTP %d: %s",
			moduleName, name, status, strings.TrimSpace(body))), nil
	}
	var objects []map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &objects); err != nil {
		return nil, Fail(moduleName + ": the device's object list is not a JSON array: " + err.Error()), nil
	}
	for _, o := range objects {
		if n, _ := o["name"].(string); n == name {
			return o, Result{}, nil
		}
	}
	return nil, Result{}, nil
}

// utmBody is real's `module.jsonify(module.params)`: every declared
// argument with its default applied, the connection arguments, and the
// url_username/url_password pair real's helper injects. See the file
// comment for why the token is in here.
func utmBody(spec utmSpec, args map[string]any, token string) string {
	out := map[string]any{}
	for _, f := range append(append([]utmField{}, utmConnectionFields...), spec.fields...) {
		if v, given := args[f.name]; given {
			out[f.name] = v
			continue
		}
		if f.def != nil {
			out[f.name] = f.def
		}
	}
	out["name"] = args["name"]
	if h, ok := args["headers"]; ok {
		out["headers"] = h
	} else {
		out["headers"] = map[string]any{}
	}
	out["url_username"] = "token"
	out["url_password"] = token
	return jsonOrEmpty(out)
}

// utmHeaders is real's _combine_headers: Accept and Content-type, with
// the task's own headers: dict merged over them.
func utmHeaders(args map[string]any) [][2]string {
	out := [][2]string{
		{"Accept", "application/json"},
		{"Content-type", "application/json"},
	}
	extra, _ := args["headers"].(map[string]any)
	for k, v := range extra {
		out = append(out, [2]string{k, fmt.Sprint(v)})
	}
	return out
}

// utmCleanResult is real's _clean_result: the device echoes the object
// back with the junk real sent it, and real deletes those keys before
// reporting. A response that is not a JSON object is returned as the
// raw string, which is what real's _read_response does for one it
// cannot parse.
func utmCleanResult(body string) any {
	var obj map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &obj); err != nil {
		return strings.TrimSpace(body)
	}
	for _, k := range []string{
		"utm_host", "utm_port", "utm_token", "utm_protocol",
		"validate_certs", "url_username", "url_password", "state",
	} {
		delete(obj, k)
	}
	return obj
}

// utmObjectChanged is real's _is_object_changed: any change-relevant key
// whose asked value differs from the device's. It also reports a key the
// device's object does not carry, which real turns into a bare
// KeyError message.
//
// The comparison is on the STRING form, because the device answers
// numbers and booleans in JSON while a task may give either a bool or
// the int the UTM backend wants -- utm_ca_host_key_cert's own source
// carries a comment about exactly that.
func utmObjectChanged(keys []string, args map[string]any, found map[string]any) (changed bool, missingKey string) {
	for _, k := range keys {
		have, ok := found[k]
		if !ok {
			return false, k
		}
		if utmCompare(args[k]) != utmCompare(have) {
			changed = true
		}
	}
	return changed, ""
}

func utmCompare(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		return strconv.FormatBool(t)
	case float64:
		// JSON numbers arrive as float64; 1 and 1.0 must compare equal
		// to an int 1 from a task.
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

// The seven registered modules. Each spec's endpoint, change keys and
// field defaults come from the module's own source, read in full.

var utmSpecs = map[string]utmSpec{
	// Write modules.
	"utm_dns_host": {
		endpoint:   "network/dns_host",
		changeKeys: []string{"comment", "hostname", "interface"},
		fields: []utmField{
			{name: "name", required: true},
			{name: "address", def: "0.0.0.0"},
			{name: "address6", def: "::"},
			{name: "comment", def: ""},
			{name: "hostname"},
			{name: "interface", def: ""},
			{name: "resolved", def: false},
			{name: "resolved6", def: false},
			{name: "timeout", def: 0},
		},
	},
	"utm_network_interface_address": {
		endpoint:   "network/interface_address",
		changeKeys: []string{"comment", "address"},
		fields: []utmField{
			{name: "name", required: true},
			{name: "address", required: true},
			{name: "comment", def: ""},
			{name: "address6"},
			{name: "resolved"},
			{name: "resolved6"},
		},
	},
	"utm_ca_host_key_cert": {
		endpoint:   "ca/host_key_cert",
		changeKeys: []string{"ca", "certificate", "comment", "encrypted", "key", "meta"},
		fields: []utmField{
			{name: "name", required: true},
			{name: "ca", required: true},
			{name: "meta", required: true},
			{name: "certificate", required: true},
			{name: "comment"},
			{name: "encrypted", def: false},
			{name: "key"},
		},
	},

	// Info modules: all four declare only name, all four support check
	// mode, and all four only ever GET.
	"utm_aaa_group_info": {
		endpoint: "aaa/group", infoOnly: true,
		fields: []utmField{{name: "name", required: true}},
	},
	"utm_ca_host_key_cert_info": {
		endpoint: "ca/host_key_cert", infoOnly: true,
		fields: []utmField{{name: "name", required: true}},
	},
	"utm_network_interface_address_info": {
		endpoint: "network/interface_address", infoOnly: true,
		fields: []utmField{{name: "name", required: true}},
	},
	"utm_proxy_frontend_info": {
		endpoint: "reverse_proxy/frontend", infoOnly: true,
		fields: []utmField{{name: "name", required: true}},
	},
	"utm_proxy_location_info": {
		endpoint: "reverse_proxy/location", infoOnly: true,
		fields: []utmField{{name: "name", required: true}},
	},
}

// Each module gets its own function rather than a closure from a table,
// for a reason that is not style: `ansible-doc <module>` prints the Go
// doc comment of the function the registry holds, and a shared closure
// would give all eight the same page. A reader asking about
// utm_dns_host should be told about utm_dns_host.
//
// Every one of them is the same three lines, and everything that differs
// is in utmSpecs above.

// moduleUtmDnsHost implements Ansible's `utm_dns_host`
// (community.general) module: a DNS host object on a Sophos UTM.
//
// Args: name (required); address (default "0.0.0.0"); address6 (default
// "::"); comment; hostname; interface; resolved; resolved6; timeout;
// plus the family's utm_host (required), utm_token (required), utm_port
// (default 4444), utm_protocol (default https) and state
// (present|absent).
//
// Only comment, hostname and interface decide whether an existing
// object is updated -- real's own key_to_check_for_changes -- so
// changing address alone on an existing host does nothing, which is
// surprising enough to be worth knowing.
func moduleUtmDnsHost(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return runUTM(ctx, conn, "utm_dns_host", utmSpecs["utm_dns_host"], args)
}

// moduleUtmNetworkInterfaceAddress implements Ansible's
// `utm_network_interface_address` (community.general) module: a network
// interface address object on a Sophos UTM.
//
// Args: name (required); address (required); comment; address6;
// resolved; resolved6; plus the family's connection arguments.
//
// comment and address decide whether an existing object is updated.
func moduleUtmNetworkInterfaceAddress(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return runUTM(ctx, conn, "utm_network_interface_address", utmSpecs["utm_network_interface_address"], args)
}

// moduleUtmCaHostKeyCert implements Ansible's `utm_ca_host_key_cert`
// (community.general) module: a host key / certificate object in the
// UTM's certificate authority.
//
// Args: name, ca, meta and certificate (all required); comment;
// encrypted (default false); key; plus the family's connection
// arguments.
//
// key is the private key, and it travels in the request body like every
// other field -- on curl's stdin, never in argv.
func moduleUtmCaHostKeyCert(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return runUTM(ctx, conn, "utm_ca_host_key_cert", utmSpecs["utm_ca_host_key_cert"], args)
}

// moduleUtmAaaGroupInfo implements Ansible's `utm_aaa_group_info`
// (community.general) module: reads one AAA group object.
//
// Args: name (required), plus the family's connection arguments. It only
// ever GETs, reports changed=false, and omits the result key entirely
// when there is no such object.
func moduleUtmAaaGroupInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return runUTM(ctx, conn, "utm_aaa_group_info", utmSpecs["utm_aaa_group_info"], args)
}

// moduleUtmCaHostKeyCertInfo implements Ansible's
// `utm_ca_host_key_cert_info` (community.general) module: reads one host
// key / certificate object. Read-only; see moduleUtmAaaGroupInfo.
func moduleUtmCaHostKeyCertInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return runUTM(ctx, conn, "utm_ca_host_key_cert_info", utmSpecs["utm_ca_host_key_cert_info"], args)
}

// moduleUtmNetworkInterfaceAddressInfo implements Ansible's
// `utm_network_interface_address_info` (community.general) module: reads
// one interface address object. Read-only; see moduleUtmAaaGroupInfo.
func moduleUtmNetworkInterfaceAddressInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return runUTM(ctx, conn, "utm_network_interface_address_info", utmSpecs["utm_network_interface_address_info"], args)
}

// moduleUtmProxyFrontendInfo implements Ansible's
// `utm_proxy_frontend_info` (community.general) module: reads one
// reverse-proxy frontend object. Read-only; see moduleUtmAaaGroupInfo.
//
// The matching WRITE module, utm_proxy_frontend, is not ported: it
// declares nineteen fields whose defaults have to be transcribed exactly
// to avoid sending a firewall a quietly different request.
func moduleUtmProxyFrontendInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return runUTM(ctx, conn, "utm_proxy_frontend_info", utmSpecs["utm_proxy_frontend_info"], args)
}

// moduleUtmProxyLocationInfo implements Ansible's
// `utm_proxy_location_info` (community.general) module: reads one
// reverse-proxy location object. Read-only; see moduleUtmAaaGroupInfo.
//
// The matching WRITE module, utm_proxy_location, is not ported, for the
// same reason as utm_proxy_frontend.
func moduleUtmProxyLocationInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return runUTM(ctx, conn, "utm_proxy_location_info", utmSpecs["utm_proxy_location_info"], args)
}
