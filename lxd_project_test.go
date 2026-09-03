package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLxdProjectMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLxdProject(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleLxdProjectCreate(t *testing.T) {
	name := "ansible-test-project"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/projects/" + name:                     {RC: 1},
		"lxc project create " + name:                              {RC: 0},
		"lxc project set " + name + " description my new project": {RC: 0},
	})
	res, err := moduleLxdProject(context.Background(), conn, map[string]any{
		"name":        name,
		"description": "my new project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleLxdProjectAlreadyPresentUnchanged(t *testing.T) {
	name := "ansible-test-project"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/projects/" + name: {RC: 0, Stdout: `{"name":"ansible-test-project","description":"my new project","config":{}}`},
	})
	res, err := moduleLxdProject(context.Background(), conn, map[string]any{
		"name":        name,
		"description": "my new project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleLxdProjectRename(t *testing.T) {
	name, newName := "ansible-test-project", "ansible-test-project-new-name"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/projects/" + name:        {RC: 0, Stdout: `{"name":"ansible-test-project","description":"","config":{}}`},
		"lxc project rename " + name + " " + newName: {RC: 0},
		"lxc query GET /1.0/projects/" + newName:     {RC: 0, Stdout: `{"name":"ansible-test-project-new-name","description":"","config":{}}`},
	})
	res, err := moduleLxdProject(context.Background(), conn, map[string]any{
		"name": name, "new_name": newName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleLxdProjectAbsent(t *testing.T) {
	name := "ansible-test-project"
	conn := newFakeConn(map[string]remoteexec.Result{
		"lxc query GET /1.0/projects/" + name: {RC: 0, Stdout: `{"name":"ansible-test-project","description":"","config":{}}`},
		"lxc project delete " + name:          {RC: 0},
	})
	res, err := moduleLxdProject(context.Background(), conn, map[string]any{
		"name": name, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
