package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const nginxStatusBody = "Active connections: 2340 \n" +
	"server accepts handled requests\n" +
	" 81769947 81769947 144332345 \n" +
	"Reading: 0 Writing: 241 Waiting: 2092 \n"

func TestModuleNginxStatusInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"curl -s -S --max-time 10 " + shellQuote("http://localhost/nginx_status"): {RC: 0, Stdout: nginxStatusBody},
	})
	res, err := moduleNginxStatusInfo(context.Background(), conn, map[string]any{"url": "http://localhost/nginx_status"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["active_connections"] != 2340 {
		t.Fatalf("active_connections = %v", res.Extra["active_connections"])
	}
	if res.Extra["accepts"] != 81769947 {
		t.Fatalf("accepts = %v", res.Extra["accepts"])
	}
	if res.Extra["handled"] != 81769947 {
		t.Fatalf("handled = %v", res.Extra["handled"])
	}
	if res.Extra["requests"] != 144332345 {
		t.Fatalf("requests = %v", res.Extra["requests"])
	}
	if res.Extra["reading"] != 0 {
		t.Fatalf("reading = %v", res.Extra["reading"])
	}
	if res.Extra["writing"] != 241 {
		t.Fatalf("writing = %v", res.Extra["writing"])
	}
	if res.Extra["waiting"] != 2092 {
		t.Fatalf("waiting = %v", res.Extra["waiting"])
	}
	if res.Extra["data"] != nginxStatusBody {
		t.Fatalf("data = %v", res.Extra["data"])
	}
}

func TestModuleNginxStatusInfoNonMatchingBody(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"curl -s -S --max-time 10 " + shellQuote("http://localhost/nginx_status"): {RC: 0, Stdout: "not a stub_status page"},
	})
	res, err := moduleNginxStatusInfo(context.Background(), conn, map[string]any{"url": "http://localhost/nginx_status"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, want success even for a non-matching body", res)
	}
	if res.Extra["active_connections"] != nil {
		t.Fatalf("active_connections = %v, want nil", res.Extra["active_connections"])
	}
}

func TestModuleNginxStatusInfoFetchFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"curl -s -S --max-time 10 " + shellQuote("http://localhost/nginx_status"): {RC: 7, Stderr: "Failed to connect"},
	})
	res, err := moduleNginxStatusInfo(context.Background(), conn, map[string]any{"url": "http://localhost/nginx_status"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed on fetch failure")
	}
}

func TestModuleNginxStatusInfoMissingURL(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleNginxStatusInfo(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing url")
	}
}

func TestModuleNginxStatusInfoCustomTimeout(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"curl -s -S --max-time 20 " + shellQuote("http://localhost/nginx_status"): {RC: 0, Stdout: nginxStatusBody},
	})
	res, err := moduleNginxStatusInfo(context.Background(), conn, map[string]any{
		"url": "http://localhost/nginx_status", "timeout": 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
