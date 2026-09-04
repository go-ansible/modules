package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakAuthenticationV2CreateWithExecution(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/flows -r myrealm":            {RC: 0, Stdout: "[]"},
		"kcadm.sh create authentication/flows -r myrealm -f - -i": {RC: 0, Stdout: "flow-1"},
		"kcadm.sh get authentication/flows/flow-1 -r myrealm": {RC: 0,
			Stdout: `{"id":"flow-1","alias":"myflow","providerId":"basic-flow"}`},
		"kcadm.sh create authentication/flows/myflow/executions/execution -r myrealm -f -": {RC: 0},
		"kcadm.sh get authentication/flows/myflow/executions -r myrealm": {RC: 0,
			Stdout: `[{"id":"exec-1","priority":0,"providerId":"auth-cookie","requirement":"DISABLED"}]`},
		"kcadm.sh update authentication/flows/myflow/executions -r myrealm -f -": {RC: 0},
	})
	res, err := moduleKeycloakAuthenticationV2(context.Background(), conn, map[string]any{
		"realm": "myrealm", "alias": "myflow", "state": "present",
		"authenticationExecutions": []any{
			map[string]any{"providerId": "auth-cookie", "requirement": "REQUIRED"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationV2AbsentMissing(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/flows -r myrealm": {RC: 0, Stdout: "[]"},
	})
	res, err := moduleKeycloakAuthenticationV2(context.Background(), conn, map[string]any{
		"realm": "myrealm", "alias": "myflow", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationV2DeleteNotInUse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/flows -r myrealm": {RC: 0,
			Stdout: `[{"id":"flow-1","alias":"myflow"}]`},
		"kcadm.sh get realms/myrealm":                            {RC: 0, Stdout: `{"realm":"myrealm"}`},
		"kcadm.sh get clients -r myrealm":                        {RC: 0, Stdout: "[]"},
		"kcadm.sh get identity-provider/instances -r myrealm":    {RC: 0, Stdout: "[]"},
		"kcadm.sh delete authentication/flows/flow-1 -r myrealm": {RC: 0},
	})
	res, err := moduleKeycloakAuthenticationV2(context.Background(), conn, map[string]any{
		"realm": "myrealm", "alias": "myflow", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationV2MissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakAuthenticationV2(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
