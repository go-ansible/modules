package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulSessionCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X PUT -d '{"Behavior":"release","LockDelay":"15s","Name":"session1"}' http://localhost:8500/v1/session/create`: {
			RC:     0,
			Stdout: "{\"ID\":\"abc-123\"}\nHTTPSTATUS:200",
		},
	})
	res, err := moduleConsulSession(context.Background(), conn, map[string]any{"name": "session1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "abc-123" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleConsulSessionDestroy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X PUT http://localhost:8500/v1/session/destroy/session_id`: {
			RC:     0,
			Stdout: "true\nHTTPSTATUS:200",
		},
	})
	res, err := moduleConsulSession(context.Background(), conn, map[string]any{"id": "session_id", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulSessionInfo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X GET http://localhost:8500/v1/session/info/session_id`: {
			RC:     0,
			Stdout: `[{"ID":"session_id","Name":"foo"}]` + "\nHTTPSTATUS:200",
		},
	})
	res, err := moduleConsulSession(context.Background(), conn, map[string]any{"id": "session_id", "state": "info"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	sessions, ok := res.Extra["sessions"].([]map[string]any)
	if !ok || len(sessions) != 1 || sessions[0]["Name"] != "foo" {
		t.Fatalf("sessions = %#v", res.Extra["sessions"])
	}
}

func TestModuleConsulSessionList(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X GET http://localhost:8500/v1/session/list`: {
			RC:     0,
			Stdout: `[]` + "\nHTTPSTATUS:200",
		},
	})
	res, err := moduleConsulSession(context.Background(), conn, map[string]any{"state": "list"})
	if err != nil {
		t.Fatal(err)
	}
	sessions, ok := res.Extra["sessions"].([]map[string]any)
	if !ok || len(sessions) != 0 {
		t.Fatalf("sessions = %#v", res.Extra["sessions"])
	}
}

func TestModuleConsulSessionRequestFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X PUT -d '{"Behavior":"release","LockDelay":"15s"}' http://localhost:8500/v1/session/create`: {
			RC:     0,
			Stdout: "permission denied\nHTTPSTATUS:403",
		},
	})
	res, err := moduleConsulSession(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-200 response")
	}
}

func TestModuleConsulSessionAbsentMissingID(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulSession(context.Background(), conn, map[string]any{"state": "absent"}); err == nil {
		t.Fatal("want error for missing id")
	}
}

func TestModuleConsulSessionBadState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulSession(context.Background(), conn, map[string]any{"state": "bogus"}); err == nil {
		t.Fatal("want error for bad state")
	}
}
