package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func hwcEcsInstanceBaseArgs() map[string]any {
	return map[string]any{
		"availability_zone": "az1",
		"flavor_name":       "s6.small.1",
		"image_id":          "image-1",
		"name":              "my-server",
		"vpc_id":            "vpc-1",
		"nics":              []any{map[string]any{"subnet_id": "subnet-1"}},
		"root_volume":       map[string]any{"volume_type": "SATA"},
	}
}

func TestModuleHwcEcsInstanceCreateSynchronousResponse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud ECS ListServers --availability_zone=az1 --flavor_name=s6.small.1 --image_id=image-1 --name=my-server --vpc_id=vpc-1": {
			RC: 0, Stdout: `{"servers":[]}`,
		},
		"hcloud ECS CreateServers --server.availability_zone=az1 --server.flavor_name=s6.small.1 --server.image_id=image-1 --server.name=my-server '--server.nics.[0].subnet_id=subnet-1' --server.root_volume.volume_type=SATA --server.vpc_id=vpc-1": {
			RC: 0, Stdout: `{"entities":{"server_id":"server-1"}}`,
		},
	})
	res, err := moduleHwcEcsInstance(context.Background(), conn, hwcEcsInstanceBaseArgs())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "server-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcEcsInstanceAdminPassNeverOnArgv(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud ECS ListServers --availability_zone=az1 --flavor_name=s6.small.1 --image_id=image-1 --name=my-server --vpc_id=vpc-1": {
			RC: 0, Stdout: `{"servers":[]}`,
		},
	})
	args := hwcEcsInstanceBaseArgs()
	args["admin_pass"] = "hunter2-secret"
	res, err := moduleHwcEcsInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	for _, cmd := range conn.Commands {
		if strings.Contains(cmd, "hunter2-secret") {
			t.Fatalf("secret admin_pass leaked onto a command line: %q", cmd)
		}
	}
	foundSecretInStdin := false
	for _, s := range conn.Stdins {
		if strings.Contains(s, "hunter2-secret") {
			foundSecretInStdin = true
		}
	}
	if !foundSecretInStdin {
		t.Fatal("expected admin_pass to be written to a temp file via stdin, none found")
	}
	foundJSONInputFlag := false
	for _, cmd := range conn.Commands {
		if strings.Contains(cmd, "--cli-jsonInput=") {
			foundJSONInputFlag = true
		}
	}
	if !foundJSONInputFlag {
		t.Fatal("expected a --cli-jsonInput= flag on the CreateServers invocation")
	}
}

func TestModuleHwcEcsInstanceMissingNics(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud ECS ListServers --availability_zone=az1 --flavor_name=s6.small.1 --image_id=image-1 --name=my-server --vpc_id=vpc-1": {
			RC: 0, Stdout: `{"servers":[]}`,
		},
	})
	args := hwcEcsInstanceBaseArgs()
	delete(args, "nics")
	_, err := moduleHwcEcsInstance(context.Background(), conn, args)
	if err == nil {
		t.Fatal("want error: missing required argument nics")
	}
}

func TestModuleHwcEcsInstanceIdempotentByID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                          {RC: 0},
		"hcloud ECS ShowServer --server_id=server-1": {RC: 0, Stdout: `{"server":{"id":"server-1","name":"my-server"}}`},
	})
	args := hwcEcsInstanceBaseArgs()
	args["id"] = "server-1"
	res, err := moduleHwcEcsInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHwcEcsInstanceAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud ECS ListServers --availability_zone=az1 --flavor_name=s6.small.1 --image_id=image-1 --name=my-server --vpc_id=vpc-1": {
			RC: 0, Stdout: `{"servers":[]}`,
		},
	})
	args := hwcEcsInstanceBaseArgs()
	args["state"] = "absent"
	res, err := moduleHwcEcsInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
