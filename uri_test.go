package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestUriCmd(t *testing.T) {
	cmd := uriCmd("GET", "https://example.com", "", nil)
	// -i so the response HEADERS come back: real builds most of its
	// result from them. -L follows redirects as real's own
	// follow_redirects default does, and url_effective is what reveals
	// whether one was followed.
	want := "curl -s -i -L -w " +
		shellQuote("\nHTTPSTATUS:%{http_code}\nHTTPTIME:%{time_total}\nHTTPURL:%{url_effective}") +
		" -X GET https://example.com"
	if cmd != want {
		t.Fatalf("cmd = %q, want %q", cmd, want)
	}

	cmd = uriCmd("POST", "https://example.com", "hi", map[string]any{"B": "2", "A": "1"})
	want = "curl -s -i -L -w " +
		shellQuote("\nHTTPSTATUS:%{http_code}\nHTTPTIME:%{time_total}\nHTTPURL:%{url_effective}") +
		" -X POST" +
		" -H " + shellQuote("A: 1") + " -H " + shellQuote("B: 2") +
		" -d hi https://example.com"
	if cmd != want {
		t.Fatalf("cmd = %q, want %q", cmd, want)
	}
}

func TestParseCurlStatus(t *testing.T) {
	body, status, err := parseCurlStatus("hello world\nHTTPSTATUS:200")
	if err != nil {
		t.Fatal(err)
	}
	if body != "hello world" || status != 200 {
		t.Fatalf("body=%q status=%d", body, status)
	}

	if _, _, err := parseCurlStatus("no marker here"); err == nil {
		t.Fatal("want error when marker is missing")
	}
	if _, _, err := parseCurlStatus("x\nHTTPSTATUS:notanumber"); err == nil {
		t.Fatal("want error for a non-numeric status")
	}
}

func TestUriStatusCodes(t *testing.T) {
	codes, err := uriStatusCodes(map[string]any{})
	if err != nil || len(codes) != 1 || codes[0] != 200 {
		t.Fatalf("codes=%v err=%v", codes, err)
	}

	codes, err = uriStatusCodes(map[string]any{"status_code": 201})
	if err != nil || len(codes) != 1 || codes[0] != 201 {
		t.Fatalf("codes=%v err=%v", codes, err)
	}

	codes, err = uriStatusCodes(map[string]any{"status_code": "204"})
	if err != nil || len(codes) != 1 || codes[0] != 204 {
		t.Fatalf("codes=%v err=%v", codes, err)
	}

	codes, err = uriStatusCodes(map[string]any{"status_code": []any{200, "201", float64(202), int64(203)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 4 || codes[0] != 200 || codes[1] != 201 || codes[2] != 202 || codes[3] != 203 {
		t.Fatalf("codes=%v", codes)
	}

	if _, err := uriStatusCodes(map[string]any{"status_code": "nope"}); err == nil {
		t.Fatal("want error for non-numeric string")
	}
	if _, err := uriStatusCodes(map[string]any{"status_code": []any{"nope"}}); err == nil {
		t.Fatal("want error for non-numeric string in list")
	}
	if _, err := uriStatusCodes(map[string]any{"status_code": []any{true}}); err == nil {
		t.Fatal("want error for unsupported type in list")
	}
	if _, err := uriStatusCodes(map[string]any{"status_code": true}); err == nil {
		t.Fatal("want error for unsupported scalar type")
	}
}

func TestModuleUriSuccessGet(t *testing.T) {
	url := "https://example.com"
	cmd := uriCmd("GET", url, "", nil)
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 13\r\n\r\n{\"ok\":true}\nHTTPSTATUS:200\nHTTPTIME:0.01\nHTTPURL:https://example.com"},
	})
	res, err := moduleURI(context.Background(), conn, map[string]any{"url": url})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["status"] != 200 {
		t.Fatalf("status = %v", res.Extra["status"])
	}
	// `content` appears only with return_content: true -- measured
	// against ansible-core 2.21.4, whose key set without it is
	// changed,content_length,content_type,cookies,cookies_string,date,
	// elapsed,failed,json,last_modified,msg,redirected,server,status,url
	// and carries no content. This test used to require it
	// unconditionally, pinning THIS PORT's own behaviour.
	if _, present := res.Extra["content"]; present {
		t.Errorf("content is reported without return_content; real omits it")
	}
	// the body still reaches a playbook as `json` when it parses as one
	if m, ok := res.Extra["json"].(map[string]any); !ok || m["ok"] != true {
		t.Errorf("json = %v, want the parsed body", res.Extra["json"])
	}
	if res.Msg != "OK (11 bytes)" {
		t.Errorf("msg = %q, want real's reason-plus-size form", res.Msg)
	}
}

func TestModuleUriPostReportsChanged(t *testing.T) {
	url := "https://example.com"
	cmd := uriCmd("POST", url, "", nil)
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "HTTP/1.1 201 Created\r\nContent-Type: text/plain\r\nContent-Length: 7\r\n\r\ncreated\nHTTPSTATUS:201\nHTTPTIME:0.01\nHTTPURL:https://example.com"},
	})
	res, err := moduleURI(context.Background(), conn, map[string]any{
		"url": url, "method": "post", "status_code": 201,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed for a POST")
	}
}

func TestModuleUriStatusMismatch(t *testing.T) {
	url := "https://example.com"
	cmd := uriCmd("GET", url, "", nil)
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "HTTP/1.1 404 Not Found\r\nContent-Type: text/plain\r\nContent-Length: 9\r\n\r\nnot found\nHTTPSTATUS:404\nHTTPTIME:0.01\nHTTPURL:https://example.com"},
	})
	res, err := moduleURI(context.Background(), conn, map[string]any{"url": url})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an unexpected status code")
	}
	if res.Extra["status"] != 404 {
		t.Fatalf("status = %v", res.Extra["status"])
	}
}

func TestModuleUriCurlFails(t *testing.T) {
	url := "https://example.com"
	cmd := uriCmd("GET", url, "", nil)
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 6, Stderr: "curl: (6) Could not resolve host"},
	})
	res, err := moduleURI(context.Background(), conn, map[string]any{"url": url})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when curl itself fails")
	}
}

func TestModuleUriMalformedResponse(t *testing.T) {
	url := "https://example.com"
	cmd := uriCmd("GET", url, "", nil)
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "no marker"},
	})
	if _, err := moduleURI(context.Background(), conn, map[string]any{"url": url}); err == nil {
		t.Fatal("want error for a malformed response")
	}
}

func TestModuleUriMissingURL(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleURI(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing url")
	}
}

func TestModuleUriBadStatusCode(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleURI(context.Background(), conn, map[string]any{
		"url": "https://x", "status_code": "nope",
	}); err == nil {
		t.Fatal("want error for invalid status_code")
	}
}

func TestModuleUriHeadersAndBody(t *testing.T) {
	url := "https://example.com"
	cmd := uriCmd("PUT", url, "payload", map[string]any{"X-Token": "abc"})
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 2\r\n\r\nok\nHTTPSTATUS:200\nHTTPTIME:0.01\nHTTPURL:https://example.com"},
	})
	res, err := moduleURI(context.Background(), conn, map[string]any{
		"url": url, "method": "PUT", "body": "payload",
		"headers": map[string]any{"X-Token": "abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
