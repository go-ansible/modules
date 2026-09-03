package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleConsulBindingRuleCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl binding-rule list -http-addr=http://localhost:8500 -format=json": {RC: 0, Stdout: "[]"},
		"consul acl binding-rule create -http-addr=http://localhost:8500 -method minikube -description " +
			shellQuote("my_name: example rule") + " -bind-type service -bind-name web -format=json": {
			RC:     0,
			Stdout: `{"ID":"br1","AuthMethod":"minikube","BindType":"service","BindName":"web","Description":"my_name: example rule"}`,
		},
	})
	res, err := moduleConsulBindingRule(context.Background(), conn, map[string]any{
		"name": "my_name", "description": "example rule", "auth_method": "minikube",
		"bind_type": "service", "bind_name": "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "create" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulBindingRuleUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl binding-rule list -http-addr=http://localhost:8500 -format=json": {
			RC: 0,
			Stdout: `[{"ID":"br1","AuthMethod":"minikube","BindType":"service","BindName":"web",` +
				`"Description":"my_name"}]`,
		},
	})
	res, err := moduleConsulBindingRule(context.Background(), conn, map[string]any{
		"name": "my_name", "auth_method": "minikube", "bind_type": "service", "bind_name": "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulBindingRuleDelete(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"consul acl binding-rule list -http-addr=http://localhost:8500 -format=json": {
			RC:     0,
			Stdout: `[{"ID":"br1","AuthMethod":"minikube","Description":"my_name"}]`,
		},
		"consul acl binding-rule delete -http-addr=http://localhost:8500 -id br1": {RC: 0},
	})
	res, err := moduleConsulBindingRule(context.Background(), conn, map[string]any{
		"name": "my_name", "auth_method": "minikube", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Extra["operation"] != "delete" {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleConsulBindingRuleBindVarsUnsupported(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleConsulBindingRule(context.Background(), conn, map[string]any{
		"name": "x", "auth_method": "m", "bind_vars": map[string]any{"a": "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for unsupported bind_vars")
	}
}

func TestModuleConsulBindingRuleMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleConsulBindingRule(context.Background(), conn, map[string]any{"name": "x"}); err == nil {
		t.Fatal("want error for missing auth_method")
	}
}
