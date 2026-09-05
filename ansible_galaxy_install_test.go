package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAnsibleGalaxyInstallCollection(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ansible-galaxy":                           {RC: 0},
		"ansible-galaxy --version":                            {RC: 0, Stdout: "ansible-galaxy [core 2.17.4]\n"},
		"ansible-galaxy collection install community.network": {RC: 0, Stdout: "community.network:5.0.0 was installed successfully\n"},
	})
	res, err := moduleAnsibleGalaxyInstall(context.Background(), conn, map[string]any{
		"type": "collection", "name": "community.network",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	nc, _ := res.Extra["new_collections"].(map[string]string)
	if nc["community.network"] != "5.0.0" {
		t.Fatalf("new_collections = %+v", res.Extra["new_collections"])
	}
	if res.Extra["version"] != "2.17.4" {
		t.Fatalf("version = %+v", res.Extra["version"])
	}
}

func TestModuleAnsibleGalaxyInstallRoleWithDestAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ansible-galaxy":                                       {RC: 0},
		"ansible-galaxy --version":                                        {RC: 0, Stdout: "ansible-galaxy [core 2.17.4]\n"},
		"ansible-galaxy role install -p /ansible/roles ansistrano.deploy": {RC: 0, Stdout: ""},
	})
	res, err := moduleAnsibleGalaxyInstall(context.Background(), conn, map[string]any{
		"type": "role", "name": "ansistrano.deploy", "dest": "/ansible/roles",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAnsibleGalaxyInstallBothRequiresRequirementsFile(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAnsibleGalaxyInstall(context.Background(), conn, map[string]any{
		"type": "both", "name": "foo",
	}); err == nil {
		t.Fatal("want error: type=both requires requirements_file, name given instead")
	}
}

func TestModuleAnsibleGalaxyInstallBothWithRequirementsFile(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ansible-galaxy":                  {RC: 0},
		"ansible-galaxy --version":                   {RC: 0, Stdout: "ansible-galaxy [core 2.17.4]\n"},
		"ansible-galaxy install -r requirements.yml": {RC: 0, Stdout: "community.general:3.1.0 was installed successfully\n- ansistrano.deploy (3.8.0) was installed successfully\n"},
	})
	res, err := moduleAnsibleGalaxyInstall(context.Background(), conn, map[string]any{
		"type": "both", "requirements_file": "requirements.yml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	nc, _ := res.Extra["new_collections"].(map[string]string)
	nr, _ := res.Extra["new_roles"].(map[string]string)
	if nc["community.general"] != "3.1.0" || nr["ansistrano.deploy"] != "3.8.0" {
		t.Fatalf("new_collections=%+v new_roles=%+v", nc, nr)
	}
}

func TestModuleAnsibleGalaxyInstallMissingBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ansible-galaxy": {RC: 1},
	})
	res, err := moduleAnsibleGalaxyInstall(context.Background(), conn, map[string]any{
		"type": "collection", "name": "community.network",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want failed, res = %+v", res)
	}
}

func TestModuleAnsibleGalaxyInstallNameAndRequirementsFileMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleAnsibleGalaxyInstall(context.Background(), conn, map[string]any{
		"type": "collection", "name": "a", "requirements_file": "r.yml",
	}); err == nil {
		t.Fatal("want error")
	}
}
