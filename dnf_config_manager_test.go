package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const dnfRepolistVerbose = `Repo-id            : appstream
Repo-name          : AppStream
Repo-status        : enabled
Repo-id            : crb
Repo-name          : CRB
Repo-status        : disabled
Repo-id            : baseos
Repo-name          : BaseOS
Repo-status        : enabled
`

func TestModuleDnfConfigManagerEnable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf repolist --all --verbose":                     {RC: 0, Stdout: dnfRepolistVerbose},
		"dnf config-manager --assumeyes --set-enabled crb": {RC: 0},
	})
	res, err := moduleDnfConfigManager(context.Background(), conn, map[string]any{"name": "crb", "state": "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	// crb stays "disabled" in our scripted repolist even after the
	// config-manager call runs (the fake connection is static), so the
	// module should report the post-check failure as a normal Result,
	// not a crash.
	if !res.Failed {
		t.Fatalf("want Failed (post-check couldn't observe the change against a static fake), res = %+v", res)
	}
}

func TestModuleDnfConfigManagerAlreadyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf repolist --all --verbose": {RC: 0, Stdout: dnfRepolistVerbose},
	})
	res, err := moduleDnfConfigManager(context.Background(), conn, map[string]any{"name": "appstream", "state": "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want only the repolist query", conn.Commands)
	}
}

func TestModuleDnfConfigManagerDisable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf repolist --all --verbose":                                   {RC: 0, Stdout: dnfRepolistVerbose},
		"dnf config-manager --assumeyes --set-disabled appstream baseos": {RC: 0},
	})
	res, err := moduleDnfConfigManager(context.Background(), conn, map[string]any{
		"name": []any{"appstream", "baseos"}, "state": "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The fake repolist is static, so the post-check will see them still
	// enabled and report Failed; what matters here is that the right
	// config-manager command was issued.
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "dnf config-manager --assumeyes --set-disabled appstream baseos" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want the set-disabled call", conn.Commands)
	}
}

func TestModuleDnfConfigManagerUnknownRepo(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf repolist --all --verbose": {RC: 0, Stdout: dnfRepolistVerbose},
	})
	res, err := moduleDnfConfigManager(context.Background(), conn, map[string]any{"name": "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed for an unknown repo ID, res = %+v", res)
	}
}

func TestModuleDnfConfigManagerNoNames(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dnf repolist --all --verbose": {RC: 0, Stdout: dnfRepolistVerbose},
	})
	res, err := moduleDnfConfigManager(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	pre, ok := res.Extra["repo_states_pre"].(map[string]any)
	if !ok {
		t.Fatalf("repo_states_pre = %#v", res.Extra["repo_states_pre"])
	}
	enabled := pre["enabled"].([]string)
	if len(enabled) != 2 {
		t.Fatalf("enabled = %v, want [appstream baseos]", enabled)
	}
}

func TestModuleDnfConfigManagerInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleDnfConfigManager(context.Background(), conn, map[string]any{"name": "crb", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}
