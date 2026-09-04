package modules

import (
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCobblerSync implements Ansible's `cobbler_sync` module: commits
// pending Cobbler changes by calling the Cobbler XML-RPC API's own
// `sync` method.
//
// Architectural note: real cobbler_sync (like real cobbler_system, see
// cobbler_system.go) never shells out to a `cobbler` CLI at all — it
// opens an `xmlrpc.client.ServerProxy` straight from the CONTROL node
// to "<proto>://<host>:<port>/cobbler_api" and calls `login` then
// `sync` over that connection (real module's own EXAMPLES show
// `delegate_to: localhost`, confirming the target of the connection is
// the Cobbler API endpoint, not whatever host the task otherwise
// targets). This port has no XML-RPC client and no direct network
// access from the Go process to arbitrary target networks — so,
// matching uri.go/apache2_mod_proxy.go's own documented convention of
// composing HTTP calls as a `curl` invocation run ON THE TARGET via
// conn.Exec, the XML-RPC request/response bodies are built/parsed in
// this port's own Go code (see xmlrpcRequest/xmlrpcMethodResponse
// below, shared with cobbler_system.go) and POSTed via curl through
// conn.Exec, so the actual login+sync round-trip happens from whatever
// host conn reaches (typically the control node itself, via a `command`/
// `shell`-equivalent local connection, matching real cobbler_sync's own
// delegate_to: localhost examples).
//
// Args: host (string, default "127.0.0.1"); port (int, optional —
// defaults to 443 if use_ssl, else 80, matching real cobbler_sync);
// username (string, default "cobbler"); password (string, optional);
// use_ssl (bool, default true); validate_certs (bool, default true) —
// false adds curl's `-k`.
//
// Real cobbler_sync always reports changed=true (its own `result =
// dict(changed=True)`, unconditionally) — this port does the same.
// Real cobbler_sync's own NOTE warns that concurrent syncs are "bound
// to fail with weird errors"; this port does not attempt to serialize
// or retry syncs, matching that same documented caveat.
func moduleCobblerSync(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	url, insecure := cobblerAPIURL(args)
	username := argString(args, "username", "cobbler")
	password := argString(args, "password", "")

	token, failMsg, err := cobblerLogin(ctx, conn, url, insecure, username, password)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	if _, err := cobblerCall(ctx, conn, url, insecure, "sync", token); err != nil {
		return Fail(fmt.Sprintf("cobbler_sync: failed to sync Cobbler. %v", err)), nil
	}

	return Changed("cobbler sync triggered"), nil
}

// cobblerAPIURL builds the Cobbler XML-RPC endpoint URL from host/port/
// use_ssl, and reports whether curl should skip certificate validation
// — matching real cobbler_sync/cobbler_system's own
// `f"{proto}://{host}:{port}/cobbler_api"` construction, including the
// same default-port-from-use_ssl behavior when port is not given.
func cobblerAPIURL(args map[string]any) (url string, insecure bool) {
	host := argString(args, "host", "127.0.0.1")
	useSSL := argBool(args, "use_ssl", true)
	proto := "https"
	defaultPort := "443"
	if !useSSL {
		proto = "http"
		defaultPort = "80"
	}
	port := argInt(args, "port", 0)
	portStr := defaultPort
	if port != 0 {
		portStr = strconv.Itoa(port)
	}
	insecure = useSSL && !argBool(args, "validate_certs", true)
	return fmt.Sprintf("%s://%s:%s/cobbler_api", proto, host, portStr), insecure
}

// cobblerLogin logs in to the Cobbler XML-RPC API and returns the
// session token. A login fault (bad credentials — real
// `xmlrpc_client.Fault`) or any other connection problem is reported
// via failMsg (a Result{Failed:true}, matching real cobbler_sync/
// cobbler_system's own two try/except branches that both call
// fail_json rather than letting the module crash); err is reserved for
// this port's own transport failure running curl at all.
func cobblerLogin(ctx context.Context, conn remoteexec.Connection, url string, insecure bool, username, password string) (token, failMsg string, err error) {
	result, err := cobblerCall(ctx, conn, url, insecure, "login", username, password)
	if err != nil {
		return "", fmt.Sprintf("cobbler: failed to log in to Cobbler %q as %q. %v", url, username, err), nil
	}
	s, _ := result.(string)
	if s == "" {
		return "", fmt.Sprintf("cobbler: failed to log in to Cobbler %q as %q: empty token returned", url, username), nil
	}
	return s, "", nil
}

// cobblerCall runs one Cobbler XML-RPC method (method, params...) by
// POSTing an XML-RPC request through curl via conn.Exec, then parsing
// the response in Go (see the doc comment on moduleCobblerSync for why
// this port composes the request as a shell curl invocation rather
// than an in-process HTTP call). Returns the single decoded result
// value, or an error for a curl-level failure, a malformed response, or
// an XML-RPC <fault>.
func cobblerCall(ctx context.Context, conn remoteexec.Connection, url string, insecure bool, method string, params ...any) (any, error) {
	body := xmlrpcRequest(method, params...)
	cmd := "curl -s -S -X POST -H " + shellQuote("Content-Type: text/xml")
	if insecure {
		cmd += " -k"
	}
	cmd += " -d " + shellQuote(body) + " " + shellQuote(url)

	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("curl to %s failed: %s", url, strings.TrimSpace(res.Stderr))
	}

	var mr xmlrpcMethodResponse
	if err := xml.Unmarshal([]byte(res.Stdout), &mr); err != nil {
		return nil, fmt.Errorf("parsing XML-RPC response to %s: %w", method, err)
	}
	if mr.Fault != nil {
		f, _ := mr.Fault.Value.toGo().(map[string]any)
		return nil, fmt.Errorf("%s: %v", method, f)
	}
	if len(mr.Params) == 0 {
		return nil, nil
	}
	return mr.Params[0].Value.toGo(), nil
}

// --- minimal XML-RPC codec, shared by cobbler_sync.go/cobbler_system.go ---
//
// This is not a general-purpose XML-RPC client: it supports exactly
// the value shapes Cobbler's own API uses in practice (string, int/i4,
// boolean, double, array, struct), enough to drive login/sync/find_
// system/get_systems/new_system/modify_system/save_system/
// remove_system/version — real Cobbler XML-RPC calls used by real
// cobbler_sync/cobbler_system (see cobbler_system.go). dateTime.iso8601
// and base64 values are not handled (Cobbler's own API does not use
// them for the methods this port calls).

// xmlrpcRequest renders a <methodCall> request body for method with
// params, each marshaled by xmlrpcMarshalValue.
func xmlrpcRequest(method string, params ...any) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	b.WriteString(xmlEscape(method))
	b.WriteString(`</methodName><params>`)
	for _, p := range params {
		b.WriteString("<param>")
		b.WriteString(xmlrpcMarshalValue(p))
		b.WriteString("</param>")
	}
	b.WriteString(`</params></methodCall>`)
	return b.String()
}

// xmlrpcMarshalValue renders v (string, bool, int, float64,
// map[string]any, or []any) as an XML-RPC <value> element.
func xmlrpcMarshalValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "<value><string></string></value>"
	case string:
		return "<value><string>" + xmlEscape(x) + "</string></value>"
	case bool:
		b := "0"
		if x {
			b = "1"
		}
		return "<value><boolean>" + b + "</boolean></value>"
	case int:
		return fmt.Sprintf("<value><int>%d</int></value>", x)
	case float64:
		return fmt.Sprintf("<value><double>%v</double></value>", x)
	case map[string]any:
		var b strings.Builder
		b.WriteString("<value><struct>")
		for k, val := range x {
			b.WriteString("<member><name>")
			b.WriteString(xmlEscape(k))
			b.WriteString("</name>")
			b.WriteString(xmlrpcMarshalValue(val))
			b.WriteString("</member>")
		}
		b.WriteString("</struct></value>")
		return b.String()
	case []any:
		var b strings.Builder
		b.WriteString("<value><array><data>")
		for _, e := range x {
			b.WriteString(xmlrpcMarshalValue(e))
		}
		b.WriteString("</data></array></value>")
		return b.String()
	case []string:
		arr := make([]any, len(x))
		for i, s := range x {
			arr[i] = s
		}
		return xmlrpcMarshalValue(arr)
	default:
		return "<value><string>" + xmlEscape(fmt.Sprint(x)) + "</string></value>"
	}
}

func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

// xmlrpcValue decodes a single XML-RPC <value> element. Exactly one of
// the typed fields is set for a typed value; an untyped <value>text
// </value> (no child element) defaults to string, per the XML-RPC spec,
// and is read from Chardata.
type xmlrpcValue struct {
	Str      *string       `xml:"string"`
	Int      *string       `xml:"int"`
	I4       *string       `xml:"i4"`
	Bool     *string       `xml:"boolean"`
	Double   *string       `xml:"double"`
	Array    *xmlrpcArray  `xml:"array"`
	Struct   *xmlrpcStruct `xml:"struct"`
	Chardata string        `xml:",chardata"`
}

type xmlrpcArray struct {
	Data []xmlrpcValue `xml:"data>value"`
}

type xmlrpcMember struct {
	Name  string      `xml:"name"`
	Value xmlrpcValue `xml:"value"`
}

type xmlrpcStruct struct {
	Members []xmlrpcMember `xml:"member"`
}

type xmlrpcParam struct {
	Value xmlrpcValue `xml:"value"`
}

type xmlrpcFault struct {
	Value xmlrpcValue `xml:"value"`
}

type xmlrpcMethodResponse struct {
	XMLName xml.Name      `xml:"methodResponse"`
	Params  []xmlrpcParam `xml:"params>param"`
	Fault   *xmlrpcFault  `xml:"fault"`
}

// toGo converts a decoded xmlrpcValue into a plain Go value: string,
// int, bool, float64, map[string]any (struct), or []any (array).
func (v xmlrpcValue) toGo() any {
	switch {
	case v.Struct != nil:
		m := make(map[string]any, len(v.Struct.Members))
		for _, mem := range v.Struct.Members {
			m[mem.Name] = mem.Value.toGo()
		}
		return m
	case v.Array != nil:
		arr := make([]any, len(v.Array.Data))
		for i, e := range v.Array.Data {
			arr[i] = e.toGo()
		}
		return arr
	case v.Str != nil:
		return *v.Str
	case v.Int != nil:
		n, _ := strconv.Atoi(strings.TrimSpace(*v.Int))
		return n
	case v.I4 != nil:
		n, _ := strconv.Atoi(strings.TrimSpace(*v.I4))
		return n
	case v.Bool != nil:
		return strings.TrimSpace(*v.Bool) == "1"
	case v.Double != nil:
		f, _ := strconv.ParseFloat(strings.TrimSpace(*v.Double), 64)
		return f
	default:
		return strings.TrimSpace(v.Chardata)
	}
}
