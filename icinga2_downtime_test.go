package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIcinga2DowntimeSchedule(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X POST -H 'Accept: application/json' -H 'X-HTTP-Method-Override: POST' -A ansible-httpget --max-time 10 -d '{"author":"Ansible","comment":"Downtime scheduled by Ansible","duration":1000,"end_time":2000,"filter":"host.name==\"host.example.com\"","start_time":1000,"type":"Host"}' https://icinga2.example.com:5665/v1/actions/schedule-downtime`: {
			RC: 0, Stdout: `{"results":[{"code":200,"legacy_id":28911,"name":"host.example.com!abc","status":"Successfully scheduled downtime."}]}` + "\nHTTPSTATUS:200",
		},
	})
	res, err := moduleIcinga2Downtime(context.Background(), conn, map[string]any{
		"url":        "https://icinga2.example.com:5665",
		"filter":     `host.name=="host.example.com"`,
		"start_time": 1000,
		"end_time":   2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	results, ok := res.Extra["results"].([]map[string]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results = %#v", res.Extra["results"])
	}
}

func TestModuleIcinga2DowntimeScheduleEndBeforeStart(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleIcinga2Downtime(context.Background(), conn, map[string]any{
		"url":        "https://icinga2.example.com:5665",
		"filter":     `host.name=="host.example.com"`,
		"start_time": 2000,
		"end_time":   1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when end_time <= start_time")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no request sent, got %v", conn.Commands)
	}
}

func TestModuleIcinga2DowntimeScheduleServerError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X POST -H 'Accept: application/json' -H 'X-HTTP-Method-Override: POST' -A ansible-httpget --max-time 10 -d '{"author":"Ansible","comment":"Downtime scheduled by Ansible","duration":1000,"end_time":2000,"filter":"host.name==\"host.example.com\"","start_time":1000,"type":"Host"}' https://icinga2.example.com:5665/v1/actions/schedule-downtime`: {
			RC: 0, Stdout: `{"error":404,"status":"No objects found."}` + "\nHTTPSTATUS:404",
		},
	})
	res, err := moduleIcinga2Downtime(context.Background(), conn, map[string]any{
		"url":        "https://icinga2.example.com:5665",
		"filter":     `host.name=="host.example.com"`,
		"start_time": 1000,
		"end_time":   2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a 404 while scheduling")
	}
	if _, ok := res.Extra["error"]; !ok {
		t.Fatal("want Extra[error] populated from the response body")
	}
}

func TestModuleIcinga2DowntimeRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X POST -H 'Accept: application/json' -H 'X-HTTP-Method-Override: POST' -A ansible-httpget --max-time 10 -d '{"downtime":"host.example.com!e19c705a-54c2-49c5-8014-70ff624f9e51","type":"Downtime"}' https://icinga2.example.com:5665/v1/actions/remove-downtime`: {
			RC: 0, Stdout: `{"results":[{"code":200}]}` + "\nHTTPSTATUS:200",
		},
	})
	res, err := moduleIcinga2Downtime(context.Background(), conn, map[string]any{
		"url":         "https://icinga2.example.com:5665",
		"state":       "absent",
		"object_type": "Downtime",
		"name":        `host.example.com!e19c705a-54c2-49c5-8014-70ff624f9e51`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIcinga2DowntimeRemoveNotFoundIsOkNotFailed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X POST -H 'Accept: application/json' -H 'X-HTTP-Method-Override: POST' -A ansible-httpget --max-time 10 -d '{"downtime":"nonexistent","type":"Downtime"}' https://icinga2.example.com:5665/v1/actions/remove-downtime`: {
			RC: 0, Stdout: `{"error":404,"status":"No objects found."}` + "\nHTTPSTATUS:404",
		},
	})
	res, err := moduleIcinga2Downtime(context.Background(), conn, map[string]any{
		"url":         "https://icinga2.example.com:5665",
		"state":       "absent",
		"object_type": "Downtime",
		"name":        "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want a 404 on removal treated as Ok, not Failed")
	}
	if res.Changed {
		t.Fatal("want a 404 on removal treated as unchanged")
	}
}

func TestModuleIcinga2DowntimeMissingFilterAndName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIcinga2Downtime(context.Background(), conn, map[string]any{
		"url": "https://icinga2.example.com:5665",
	}); err == nil {
		t.Fatal("want error when neither filter nor name is given")
	}
}

func TestModuleIcinga2DowntimeMissingURL(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIcinga2Downtime(context.Background(), conn, map[string]any{
		"filter": `host.name=="x"`,
	}); err == nil {
		t.Fatal("want error for missing url")
	}
}

func TestModuleIcinga2DowntimeBadObjectType(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIcinga2Downtime(context.Background(), conn, map[string]any{
		"url": "https://icinga2.example.com:5665", "filter": `host.name=="x"`, "object_type": "Bogus",
	}); err == nil {
		t.Fatal("want error for bad object_type")
	}
}
