package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakAuthenticationRequiredActionsRegister(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/required-actions -r myrealm":                 {RC: 0, Stdout: "[]"},
		"kcadm.sh create authentication/register-required-action -r myrealm -f -": {RC: 0},
		"kcadm.sh get authentication/required-actions/TERMS_AND_CONDITIONS -r myrealm": {RC: 0,
			Stdout: `{"alias":"TERMS_AND_CONDITIONS","name":"Terms","providerId":"TERMS_AND_CONDITIONS","enabled":true}`},
	})
	res, err := moduleKeycloakAuthenticationRequiredActions(context.Background(), conn, map[string]any{
		"realm": "myrealm", "state": "present",
		"required_actions": []any{
			map[string]any{"alias": "TERMS_AND_CONDITIONS", "name": "Terms", "providerId": "TERMS_AND_CONDITIONS", "enabled": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationRequiredActionsAlreadyUpToDate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/required-actions -r myrealm": {RC: 0,
			Stdout: `[{"alias":"TERMS_AND_CONDITIONS","enabled":false}]`},
	})
	res, err := moduleKeycloakAuthenticationRequiredActions(context.Background(), conn, map[string]any{
		"realm": "myrealm", "state": "present",
		"required_actions": []any{
			map[string]any{"alias": "TERMS_AND_CONDITIONS", "enabled": false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationRequiredActionsDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/required-actions -r myrealm": {RC: 0,
			Stdout: `[{"alias":"TERMS_AND_CONDITIONS"}]`},
		"kcadm.sh delete authentication/required-actions/TERMS_AND_CONDITIONS -r myrealm": {RC: 0},
	})
	res, err := moduleKeycloakAuthenticationRequiredActions(context.Background(), conn, map[string]any{
		"realm": "myrealm", "state": "absent",
		"required_actions": []any{map[string]any{"alias": "TERMS_AND_CONDITIONS"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakAuthenticationRequiredActionsDedupe(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get authentication/required-actions -r myrealm": {RC: 0,
			Stdout: `[{"alias":"TERMS_AND_CONDITIONS","enabled":false}]`},
	})
	res, err := moduleKeycloakAuthenticationRequiredActions(context.Background(), conn, map[string]any{
		"realm": "myrealm", "state": "present",
		"required_actions": []any{
			map[string]any{"alias": "TERMS_AND_CONDITIONS", "enabled": false},
			map[string]any{"alias": "TERMS_AND_CONDITIONS", "enabled": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// first occurrence (enabled:false) should win and match existing -> no change
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
