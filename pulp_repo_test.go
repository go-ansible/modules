package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePulpRepoCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v pulp":                     {RC: 0},
		"pulp rpm remote show --name my_repo": {RC: 1},
		"pulp rpm remote create --name my_repo --url http://mirror.centos.org/centos/6/updates/x86_64/ --policy immediate": {RC: 0},
		"pulp rpm repository show --name my_repo":                                                       {RC: 1},
		"pulp rpm repository create --name my_repo --remote my_repo":                                    {RC: 0},
		"pulp rpm distribution show --name my_repo":                                                     {RC: 1},
		"pulp rpm distribution create --name my_repo --repository my_repo --base-path centos/6/updates": {RC: 0},
	})
	res, err := modulePulpRepo(context.Background(), conn, map[string]any{
		"name":         "my_repo",
		"repo_type":    "rpm",
		"feed":         "http://mirror.centos.org/centos/6/updates/x86_64/",
		"relative_url": "centos/6/updates",
		"state":        "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want Changed=true")
	}
	if res.Extra["repo"] != "my_repo" {
		t.Fatalf("repo = %+v", res.Extra["repo"])
	}
}

func TestModulePulpRepoPresentAlreadyExists(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v pulp":                           {RC: 0},
		"pulp rpm repository show --name my_repo":   {RC: 0, Stdout: `{"name":"my_repo"}`},
		"pulp rpm distribution show --name my_repo": {RC: 0, Stdout: `{"name":"my_repo"}`},
	})
	res, err := modulePulpRepo(context.Background(), conn, map[string]any{
		"name":         "my_repo",
		"relative_url": "my/repo",
		"state":        "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePulpRepoAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v pulp": {RC: 0},
		"pulp rpm distribution show --name my_old_repo":  {RC: 1},
		"pulp rpm repository show --name my_old_repo":    {RC: 0, Stdout: `{"name":"my_old_repo"}`},
		"pulp rpm repository destroy --name my_old_repo": {RC: 0},
		"pulp rpm remote show --name my_old_repo":        {RC: 1},
	})
	res, err := modulePulpRepo(context.Background(), conn, map[string]any{
		"name":  "my_old_repo",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePulpRepoSync(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v pulp":                         {RC: 0},
		"pulp rpm repository sync --name my_repo": {RC: 0},
	})
	res, err := modulePulpRepo(context.Background(), conn, map[string]any{
		"name":  "my_repo",
		"state": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePulpRepoPublish(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v pulp": {RC: 0},
		"pulp rpm publication create --repository my_repo": {RC: 0},
	})
	res, err := modulePulpRepo(context.Background(), conn, map[string]any{
		"name":  "my_repo",
		"state": "publish",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModulePulpRepoUnsupportedRepoType(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := modulePulpRepo(context.Background(), conn, map[string]any{
		"name":      "my_repo",
		"repo_type": "docker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed for unsupported repo_type, res = %+v", res)
	}
}

func TestModulePulpRepoMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := modulePulpRepo(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing name")
	}
}
