package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const icinga2StatusOKCmd = "curl -s -w '\nHTTPSTATUS:%{http_code}' -X GET -H 'Accept: application/json' -H 'X-HTTP-Method-Override: GET' -A ansible-httpget https://icinga2.example.com/v1/status"

const icinga2HostExistsCmd = `curl -s -w '
HTTPSTATUS:%{http_code}' -X GET -H 'Accept: application/json' -H 'X-HTTP-Method-Override: GET' -A ansible-httpget -d '{"filter":"match(\"myhost\", host.name)"}' https://icinga2.example.com/v1/objects/hosts`

func TestModuleIcinga2HostCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		icinga2StatusOKCmd:   {RC: 0, Stdout: "{}\nHTTPSTATUS:200"},
		icinga2HostExistsCmd: {RC: 0, Stdout: `{"results":[]}` + "\nHTTPSTATUS:200"},
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X PUT -H 'Accept: application/json' -H 'X-HTTP-Method-Override: PUT' -A ansible-httpget -d '{"attrs":{"address":"10.0.0.1","check_command":"hostalive","display_name":"myhost","vars.made_by":"ansible","zone":""},"templates":[]}' https://icinga2.example.com/v1/objects/hosts/myhost`: {
			RC: 0, Stdout: `{"results":[{"code":200}]}` + "\nHTTPSTATUS:200",
		},
	})
	res, err := moduleIcinga2Host(context.Background(), conn, map[string]any{
		"url": "https://icinga2.example.com", "name": "myhost", "ip": "10.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	if res.Extra["name"] != "myhost" {
		t.Fatalf("name = %v", res.Extra["name"])
	}
}

func TestModuleIcinga2HostAlreadyExistsNoDiff(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		icinga2StatusOKCmd:   {RC: 0, Stdout: "{}\nHTTPSTATUS:200"},
		icinga2HostExistsCmd: {RC: 0, Stdout: `{"results":[{"name":"myhost"}]}` + "\nHTTPSTATUS:200"},
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X GET -H 'Accept: application/json' -H 'X-HTTP-Method-Override: GET' -A ansible-httpget https://icinga2.example.com/v1/objects/hosts/myhost`: {
			RC: 0, Stdout: `{"results":[{"attrs":{"address":"10.0.0.1","check_command":"hostalive","display_name":"myhost","vars.made_by":"ansible","zone":""}}]}` + "\nHTTPSTATUS:200",
		},
	})
	res, err := moduleIcinga2Host(context.Background(), conn, map[string]any{
		"url": "https://icinga2.example.com", "name": "myhost", "ip": "10.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIcinga2HostModifyOnDiff(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		icinga2StatusOKCmd:   {RC: 0, Stdout: "{}\nHTTPSTATUS:200"},
		icinga2HostExistsCmd: {RC: 0, Stdout: `{"results":[{"name":"myhost"}]}` + "\nHTTPSTATUS:200"},
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X GET -H 'Accept: application/json' -H 'X-HTTP-Method-Override: GET' -A ansible-httpget https://icinga2.example.com/v1/objects/hosts/myhost`: {
			RC: 0, Stdout: `{"results":[{"attrs":{"address":"10.0.0.2","check_command":"hostalive","display_name":"myhost","vars.made_by":"ansible","zone":""}}]}` + "\nHTTPSTATUS:200",
		},
		`curl -s -w '
HTTPSTATUS:%{http_code}' -X POST -H 'Accept: application/json' -H 'X-HTTP-Method-Override: POST' -A ansible-httpget -d '{"attrs":{"address":"10.0.0.1","check_command":"hostalive","display_name":"myhost","vars.made_by":"ansible","zone":""}}' https://icinga2.example.com/v1/objects/hosts/myhost`: {
			RC: 0, Stdout: `{"results":[{"code":200}]}` + "\nHTTPSTATUS:200",
		},
	})
	res, err := moduleIcinga2Host(context.Background(), conn, map[string]any{
		"url": "https://icinga2.example.com", "name": "myhost", "ip": "10.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	if data, ok := res.Extra["data"].(map[string]any); !ok || data["templates"] != nil {
		t.Fatalf("modify data should not carry templates: %#v", res.Extra["data"])
	}
}

func TestModuleIcinga2HostDeleteWhenAbsentIsNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		icinga2StatusOKCmd:   {RC: 0, Stdout: "{}\nHTTPSTATUS:200"},
		icinga2HostExistsCmd: {RC: 0, Stdout: `{"results":[]}` + "\nHTTPSTATUS:200"},
	})
	res, err := moduleIcinga2Host(context.Background(), conn, map[string]any{
		"url": "https://icinga2.example.com", "name": "myhost", "ip": "10.0.0.1", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleIcinga2HostConnectFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		icinga2StatusOKCmd: {RC: 1, Stderr: "connection refused"},
	})
	res, err := moduleIcinga2Host(context.Background(), conn, map[string]any{
		"url": "https://icinga2.example.com", "name": "myhost",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the connectivity probe fails")
	}
}

func TestModuleIcinga2HostMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIcinga2Host(context.Background(), conn, map[string]any{"url": "https://icinga2.example.com"}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleIcinga2Host(context.Background(), conn, map[string]any{"name": "myhost"}); err == nil {
		t.Fatal("want error for missing url")
	}
}
