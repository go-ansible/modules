package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRundeckACLPolicyCreateSystem(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 0},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd system acls get -n Project_01":         {RC: 1},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd system acls create -n Project_01 -f -": {RC: 0},
	})
	res, err := moduleRundeckACLPolicy(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok",
		"policy": "description: my policy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if len(conn.Stdins) == 0 || conn.Stdins[len(conn.Stdins)-1] != "description: my policy" {
		t.Fatalf("stdins = %v", conn.Stdins)
	}
}

func TestModuleRundeckACLPolicyCreateProjectScoped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 0},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd projects acls get -p Ansible -n Project_01":         {RC: 1},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd projects acls create -p Ansible -n Project_01 -f -": {RC: 0},
	})
	res, err := moduleRundeckACLPolicy(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok",
		"project": "Ansible", "policy": "description: my policy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRundeckACLPolicyUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 0},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd system acls get -n Project_01": {RC: 0, Stdout: "description: my policy"},
	})
	res, err := moduleRundeckACLPolicy(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok",
		"policy": "description: my policy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: content matches")
	}
}

func TestModuleRundeckACLPolicyUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 0},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd system acls get -n Project_01":         {RC: 0, Stdout: "old"},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd system acls update -n Project_01 -f -": {RC: 0},
	})
	res, err := moduleRundeckACLPolicy(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok",
		"policy": "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRundeckACLPolicyRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v rd": {RC: 0},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd system acls get -n Project_01":    {RC: 0, Stdout: "x"},
		"RD_FORMAT=json RD_URL=https://rundeck.example.org RD_TOKEN=tok rd system acls delete -n Project_01": {RC: 0},
	})
	res, err := moduleRundeckACLPolicy(context.Background(), conn, map[string]any{
		"name": "Project_01", "url": "https://rundeck.example.org", "api_token": "tok", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRundeckACLPolicyMappingPolicy(t *testing.T) {
	got, err := rundeckACLPolicyText(map[string]any{
		"policy": map[string]any{"description": "my policy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "description: my policy\n" {
		t.Fatalf("policy text = %q", got)
	}
}

func TestModuleRundeckACLPolicyValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleRundeckACLPolicy(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleRundeckACLPolicy(context.Background(), conn, map[string]any{
		"name": "!bad", "url": "u", "api_token": "t",
	}); err == nil {
		t.Fatal("want error for a name starting with a forbidden character")
	}
	if _, err := moduleRundeckACLPolicy(context.Background(), conn, map[string]any{
		"name": "ok", "url": "u", "api_token": "t",
	}); err == nil {
		t.Fatal("want error: policy required for present")
	}
}
