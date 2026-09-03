package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const rhsmRepoListOutput = `+----------------------------------------------------------+
    Available Repositories in /etc/yum.repos.d/redhat.repo
+----------------------------------------------------------+
Repo ID:   rhel-7-server-rpms
Repo Name: Red Hat Enterprise Linux 7 Server (RPMs)
Repo URL:  https://cdn.redhat.com/content/dist/rhel/server/7/$releasever/$basearch/os
Enabled:   1

Repo ID:   rhel-7-server-optional-rpms
Repo Name: Red Hat Enterprise Linux 7 Server - Optional (RPMs)
Repo URL:  https://cdn.redhat.com/content/dist/rhel/server/7/$releasever/$basearch/optional/os
Enabled:   0
`

func TestModuleRhsmRepositoryEnable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                             {RC: 0, Stdout: "0\n"},
		"subscription-manager repos --list": {RC: 0, Stdout: rhsmRepoListOutput},
		"subscription-manager repos --enable " + shellQuote("rhel-7-server-optional-rpms"): {RC: 0},
	})
	res, err := moduleRhsmRepository(context.Background(), conn, map[string]any{
		"name": []any{"rhel-7-server-optional-rpms"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRhsmRepositoryAlreadyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                             {RC: 0, Stdout: "0\n"},
		"subscription-manager repos --list": {RC: 0, Stdout: rhsmRepoListOutput},
	})
	res, err := moduleRhsmRepository(context.Background(), conn, map[string]any{
		"name": []any{"rhel-7-server-rpms"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRhsmRepositoryUnknownID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                             {RC: 0, Stdout: "0\n"},
		"subscription-manager repos --list": {RC: 0, Stdout: rhsmRepoListOutput},
	})
	res, err := moduleRhsmRepository(context.Background(), conn, map[string]any{
		"name": []any{"does-not-exist"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an unknown repo ID")
	}
}

func TestModuleRhsmRepositoryNotRoot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u": {RC: 0, Stdout: "1000\n"},
	})
	res, err := moduleRhsmRepository(context.Background(), conn, map[string]any{"name": []any{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when not root")
	}
}

func TestModuleRhsmRepositoryMissingName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{"id -u": {RC: 0, Stdout: "0\n"}})
	if _, err := moduleRhsmRepository(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleRhsmRepositoryGlobMatch(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                             {RC: 0, Stdout: "0\n"},
		"subscription-manager repos --list": {RC: 0, Stdout: rhsmRepoListOutput},
		"subscription-manager repos --disable " + shellQuote("rhel-7-server-rpms"): {RC: 0},
	})
	res, err := moduleRhsmRepository(context.Background(), conn, map[string]any{
		"name": []any{"rhel-7-server-rpms"}, "state": "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
