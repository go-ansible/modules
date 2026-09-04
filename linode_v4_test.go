package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLinodeV4Create(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v linode-cli":                               {RC: 0},
		"LINODE_CLI_TOKEN=tok linode-cli linodes list --json": {RC: 0, Stdout: `[]`},
		"LINODE_CLI_TOKEN=tok linode-cli linodes create --label my-linode --region us-east --image linode/debian11 --type g6-nanode-1 --json": {
			RC: 0, Stdout: `{"id":123,"label":"my-linode","region":"us-east"}`,
		},
	})
	args := map[string]any{
		"label": "my-linode", "state": "present", "access_token": "tok",
		"region": "us-east", "image": "linode/debian11", "type": "g6-nanode-1",
	}
	res, err := moduleLinodeV4(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	instance, ok := res.Extra["instance"].(map[string]any)
	if !ok || instance["id"] != float64(123) {
		t.Fatalf("instance = %+v", res.Extra["instance"])
	}
}

func TestModuleLinodeV4Idempotent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v linode-cli":                               {RC: 0},
		"LINODE_CLI_TOKEN=tok linode-cli linodes list --json": {RC: 0, Stdout: `[{"id":123,"label":"my-linode"}]`},
	})
	args := map[string]any{"label": "my-linode", "state": "present", "access_token": "tok"}
	res, err := moduleLinodeV4(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLinodeV4AbsentDeletes(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v linode-cli":                               {RC: 0},
		"LINODE_CLI_TOKEN=tok linode-cli linodes list --json": {RC: 0, Stdout: `[{"id":123,"label":"my-linode"}]`},
		"LINODE_CLI_TOKEN=tok linode-cli linodes delete 123":  {RC: 0},
	})
	args := map[string]any{"label": "my-linode", "state": "absent", "access_token": "tok"}
	res, err := moduleLinodeV4(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLinodeV4AbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v linode-cli":                               {RC: 0},
		"LINODE_CLI_TOKEN=tok linode-cli linodes list --json": {RC: 0, Stdout: `[]`},
	})
	args := map[string]any{"label": "my-linode", "state": "absent", "access_token": "tok"}
	res, err := moduleLinodeV4(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleLinodeV4RootPassNeverOnArgv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v linode-cli":                               {RC: 0},
		"LINODE_CLI_TOKEN=tok linode-cli linodes list --json": {RC: 0, Stdout: `[]`},
		"LINODE_CLI_TOKEN=tok linode-cli linodes create --label my-linode --region us-east --image linode/debian11 --type g6-nanode-1 --root_pass --json": {
			RC: 0, Stdout: `{"id":123,"label":"my-linode"}`,
		},
	})
	args := map[string]any{
		"label": "my-linode", "state": "present", "access_token": "tok",
		"region": "us-east", "image": "linode/debian11", "type": "g6-nanode-1",
		"root_pass": "hunter2-secret",
	}
	res, err := moduleLinodeV4(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	for _, cmd := range conn.Commands {
		if strings.Contains(cmd, "hunter2-secret") {
			t.Fatalf("secret root_pass leaked onto a command line: %q", cmd)
		}
	}
	found := false
	for _, s := range conn.Stdins {
		if strings.Contains(s, "hunter2-secret") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected root_pass to be piped over stdin, none found")
	}
}
