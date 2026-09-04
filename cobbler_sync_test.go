package modules

import (
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// cobblerFakeConn is a scripted connection for cobbler_sync/
// cobbler_system tests. It does not match the whole curl command line
// verbatim (that would mean hardcoding a full XML-RPC request body in
// every test, which is both unreadable and brittle against harmless
// wording changes in cobblerCall); instead it extracts the XML-RPC
// <methodName> embedded in the command and dispatches on that,
// recording every methodName invoked, in order, for assertions.
type cobblerFakeConn struct {
	// on maps an XML-RPC method name to a queue of canned <methodResponse>
	// XML bodies. A queue with one entry repeats it for every call to
	// that method; a queue with more than one is consumed in order and
	// then repeats its last entry.
	on map[string][]string
	// failCall, if non-empty, is a method name whose call is scripted to
	// look like a curl-level failure (RC != 0), independent of any
	// XML-RPC-level fault.
	failCall string

	Calls []string
}

func (f *cobblerFakeConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	method := cobblerExtractMethodName(cmd)
	f.Calls = append(f.Calls, method)
	if f.failCall != "" && method == f.failCall {
		return remoteexec.Result{RC: 1, Stderr: "curl: (7) Failed to connect"}, nil
	}
	if queue, ok := f.on[method]; ok && len(queue) > 0 {
		resp := queue[0]
		if len(queue) > 1 {
			f.on[method] = queue[1:]
		}
		return remoteexec.Result{RC: 0, Stdout: resp}, nil
	}
	return remoteexec.Result{RC: 0, Stdout: xmlrpcStringResponse("")}, nil
}

func (f *cobblerFakeConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return nil
}
func (f *cobblerFakeConn) Fetch(ctx context.Context, remotePath, localPath string) error { return nil }
func (f *cobblerFakeConn) Remove(ctx context.Context, remotePath string) error           { return nil }
func (f *cobblerFakeConn) TempPath(base string) string                                   { return "/tmp/" + base }
func (f *cobblerFakeConn) Close() error                                                  { return nil }

var _ remoteexec.Connection = (*cobblerFakeConn)(nil)

func cobblerExtractMethodName(cmd string) string {
	i := strings.Index(cmd, "<methodName>")
	if i < 0 {
		return ""
	}
	rest := cmd[i+len("<methodName>"):]
	j := strings.Index(rest, "</methodName>")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func xmlrpcStringResponse(s string) string {
	return `<?xml version="1.0"?><methodResponse><params><param><value><string>` + s + `</string></value></param></params></methodResponse>`
}

func xmlrpcBoolResponse(b bool) string {
	v := "0"
	if b {
		v = "1"
	}
	return `<?xml version="1.0"?><methodResponse><params><param><value><boolean>` + v + `</boolean></value></param></params></methodResponse>`
}

func xmlrpcFaultResponse(msg string) string {
	return `<?xml version="1.0"?><methodResponse><fault><value><struct>` +
		`<member><name>faultCode</name><value><int>1</int></value></member>` +
		`<member><name>faultString</name><value><string>` + msg + `</string></value></member>` +
		`</struct></value></fault></methodResponse>`
}

func xmlrpcArrayResponse(items ...string) string {
	return `<?xml version="1.0"?><methodResponse><params><param><value><array><data>` +
		strings.Join(items, "") + `</data></array></value></param></params></methodResponse>`
}

func TestModuleCobblerSyncBasic(t *testing.T) {
	conn := &cobblerFakeConn{on: map[string][]string{
		"login": {xmlrpcStringResponse("TOKEN123")},
		"sync":  {xmlrpcBoolResponse(true)},
	}}
	res, err := moduleCobblerSync(context.Background(), conn, map[string]any{
		"host": "cobbler01", "username": "cobbler", "password": "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Calls) != 2 || conn.Calls[0] != "login" || conn.Calls[1] != "sync" {
		t.Fatalf("calls = %v, want [login sync]", conn.Calls)
	}
}

func TestModuleCobblerSyncLoginFault(t *testing.T) {
	conn := &cobblerFakeConn{on: map[string][]string{
		"login": {xmlrpcFaultResponse("invalid credentials")},
	}}
	res, err := moduleCobblerSync(context.Background(), conn, map[string]any{"host": "cobbler01"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a login fault")
	}
}

func TestModuleCobblerSyncConnectionDown(t *testing.T) {
	conn := &cobblerFakeConn{failCall: "login"}
	res, err := moduleCobblerSync(context.Background(), conn, map[string]any{"host": "cobbler01"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when curl cannot reach Cobbler")
	}
}

func TestModuleCobblerSyncFault(t *testing.T) {
	conn := &cobblerFakeConn{on: map[string][]string{
		"login": {xmlrpcStringResponse("TOKEN")},
		"sync":  {xmlrpcFaultResponse("sync already running")},
	}}
	res, err := moduleCobblerSync(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a sync fault")
	}
}

func TestCobblerAPIURL(t *testing.T) {
	url, insecure := cobblerAPIURL(map[string]any{})
	if url != "https://127.0.0.1:443/cobbler_api" || insecure {
		t.Fatalf("url = %q insecure = %v", url, insecure)
	}

	url, insecure = cobblerAPIURL(map[string]any{"use_ssl": false})
	if url != "http://127.0.0.1:80/cobbler_api" || insecure {
		t.Fatalf("url = %q insecure = %v", url, insecure)
	}

	url, insecure = cobblerAPIURL(map[string]any{"host": "cb01", "port": 8443, "validate_certs": false})
	if url != "https://cb01:8443/cobbler_api" || !insecure {
		t.Fatalf("url = %q insecure = %v", url, insecure)
	}
}

func TestXMLRPCValueRoundTrip(t *testing.T) {
	resp := `<?xml version="1.0"?><methodResponse><params><param><value><struct>` +
		`<member><name>name</name><value><string>myhost</string></value></member>` +
		`<member><name>netboot_enabled</name><value><boolean>1</boolean></value></member>` +
		`<member><name>id</name><value><int>42</int></value></member>` +
		`<member><name>tags</name><value><array><data><value><string>a</string></value><value><string>b</string></value></data></array></value></member>` +
		`</struct></value></param></params></methodResponse>`
	var mr xmlrpcMethodResponse
	if err := xml.Unmarshal([]byte(resp), &mr); err != nil {
		t.Fatal(err)
	}
	got, ok := mr.Params[0].Value.toGo().(map[string]any)
	if !ok {
		t.Fatalf("not a struct: %#v", mr.Params[0].Value.toGo())
	}
	if got["name"] != "myhost" {
		t.Errorf("name = %v", got["name"])
	}
	if got["netboot_enabled"] != true {
		t.Errorf("netboot_enabled = %v", got["netboot_enabled"])
	}
	if got["id"] != 42 {
		t.Errorf("id = %v", got["id"])
	}
	tags, _ := got["tags"].([]any)
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags = %v", tags)
	}
}

func TestXMLRPCMarshalWellFormed(t *testing.T) {
	body := xmlrpcRequest("modify_system", "sys-1", "properties",
		map[string]any{"profile": "CentOS", "count": 3}, "TOKEN")
	if !strings.Contains(body, "<methodName>modify_system</methodName>") {
		t.Fatalf("body missing methodName: %s", body)
	}
	var mc struct {
		XMLName xml.Name `xml:"methodCall"`
		Params  []struct {
			Value xmlrpcValue `xml:"value"`
		} `xml:"params>param"`
	}
	if err := xml.Unmarshal([]byte(body), &mc); err != nil {
		t.Fatalf("marshaled request is not well-formed XML: %v\n%s", err, body)
	}
	if len(mc.Params) != 4 {
		t.Fatalf("params = %d, want 4", len(mc.Params))
	}
}
