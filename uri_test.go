package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestUriCmd(t *testing.T) {
	cmd := uriCmd("GET", "https://example.com", "", nil)
	want := "curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}") + " -X GET https://example.com"
	if cmd != want {
		t.Fatalf("cmd = %q, want %q", cmd, want)
	}

	cmd = uriCmd("POST", "https://example.com", "hi", map[string]any{"B": "2", "A": "1"})
	want = "curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}") + " -X POST" +
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
		cmd: {RC: 0, Stdout: "{\"ok\":true}\nHTTPSTATUS:200"},
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
	if res.Extra["content"] != "{\"ok\":true}" {
		t.Fatalf("content = %v", res.Extra["content"])
	}
}

func TestModuleUriPostReportsChanged(t *testing.T) {
	url := "https://example.com"
	cmd := uriCmd("POST", url, "", nil)
	conn := newFakeConn(map[string]remoteexec.Result{
		cmd: {RC: 0, Stdout: "created\nHTTPSTATUS:201"},
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
		cmd: {RC: 0, Stdout: "not found\nHTTPSTATUS:404"},
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
		cmd: {RC: 0, Stdout: "ok\nHTTPSTATUS:200"},
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
