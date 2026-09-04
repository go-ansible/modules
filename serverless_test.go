package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const serverlessYmlBasic = "service: myservice\nprovider:\n  name: aws\n"

func TestModuleServerlessDeploy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cd /svc && serverless deploy": {RC: 0, Stdout: "Service deployed\n"},
		"cat /svc/serverless.yml":      {RC: 0, Stdout: serverlessYmlBasic},
	})
	res, err := moduleServerless(context.Background(), conn, map[string]any{"service_path": "/svc"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	// Real serverless.py hardcodes "present" here even for a successful
	// `remove` — see moduleServerless's own doc comment; for `deploy`
	// (state=present) this happens to be the actually-correct value too.
	if res.Extra["state"] != "present" {
		t.Fatalf("state = %v", res.Extra["state"])
	}
	if res.Extra["service_name"] != "myservice-dev" {
		t.Fatalf("service_name = %v", res.Extra["service_name"])
	}
	if res.Extra["command"] != "serverless deploy" {
		t.Fatalf("command = %v", res.Extra["command"])
	}
}

func TestModuleServerlessRemoveNotDeployed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cd /svc && serverless remove --stage prod": {RC: 1, Stdout: "Stack 'myservice-prod' does not exist\n"},
		"cat /svc/serverless.yml":                   {RC: 0, Stdout: serverlessYmlBasic},
	})
	res, err := moduleServerless(context.Background(), conn, map[string]any{
		"service_path": "/svc", "state": "absent", "stage": "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["state"] != "absent" {
		t.Fatalf("state = %v", res.Extra["state"])
	}
	if res.Extra["service_name"] != "myservice-prod" {
		t.Fatalf("service_name = %v", res.Extra["service_name"])
	}
}

func TestModuleServerlessGenuineFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cd /svc && serverless deploy": {RC: 1, Stdout: "", Stderr: "boom"},
	})
	res, err := moduleServerless(context.Background(), conn, map[string]any{"service_path": "/svc"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleServerlessMissingServiceKey(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cd /svc && serverless deploy": {RC: 0, Stdout: "ok\n"},
		"cat /svc/serverless.yml":      {RC: 0, Stdout: "provider:\n  name: aws\n"},
	})
	if _, err := moduleServerless(context.Background(), conn, map[string]any{"service_path": "/svc"}); err == nil {
		t.Fatal("want error when serverless.yml has no `service` key")
	}
}

func TestModuleServerlessMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleServerless(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing service_path")
	}
}
