package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKeycloakClienttemplateCreate(t *testing.T) {
	conn := newSeqConn(map[string][]remoteexec.Result{
		"kcadm.sh get client-templates -r myrealm": {
			{RC: 0, Stdout: "[]"},
			{RC: 0, Stdout: `[{"id":"ct-1","name":"test01"}]`},
		},
		"kcadm.sh create client-templates -r myrealm -f -": {{RC: 0}},
	})
	res, err := moduleKeycloakClienttemplate(context.Background(), conn, map[string]any{
		"realm": "myrealm", "name": "test01", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClienttemplateNoChangesRequired(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get client-templates -r myrealm": {RC: 0,
			Stdout: `[{"id":"ct-1","name":"test01","protocol":"saml"}]`},
	})
	res, err := moduleKeycloakClienttemplate(context.Background(), conn, map[string]any{
		"realm": "myrealm", "name": "test01", "protocol": "saml", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClienttemplateDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"kcadm.sh get client-templates -r myrealm": {RC: 0,
			Stdout: `[{"id":"ct-1","name":"test01"}]`},
		"kcadm.sh delete client-templates/ct-1 -r myrealm": {RC: 0},
	})
	res, err := moduleKeycloakClienttemplate(context.Background(), conn, map[string]any{
		"realm": "myrealm", "name": "test01", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleKeycloakClienttemplateMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKeycloakClienttemplate(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing required args")
	}
}
