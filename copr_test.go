package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleCoprEnable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/yum.repos.d/_copr:copr.fedorainfracloud.org:schlupov:Test.repo": {RC: 1},
		"dnf -y copr enable schlupov/Test fedora-31-x86_64":                           {RC: 0},
	})
	res, err := moduleCopr(context.Background(), conn, map[string]any{
		"name":   "schlupov/Test",
		"chroot": "fedora-31-x86_64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["repo_filename"] != "_copr:copr.fedorainfracloud.org:schlupov:Test.repo" {
		t.Fatalf("repo_filename = %v", res.Extra["repo_filename"])
	}
}

func TestModuleCoprAlreadyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/yum.repos.d/_copr:copr.fedorainfracloud.org:schlupov:Test.repo":              {RC: 0},
		"grep -q '^enabled=1' /etc/yum.repos.d/_copr:copr.fedorainfracloud.org:schlupov:Test.repo": {RC: 0},
	})
	res, err := moduleCopr(context.Background(), conn, map[string]any{"name": "schlupov/Test"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleCoprRemoveAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/yum.repos.d/_copr:copr.fedorainfracloud.org:group_copr:integration_tests.repo": {RC: 1},
	})
	res, err := moduleCopr(context.Background(), conn, map[string]any{
		"name":  "@copr/integration_tests",
		"state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged (already absent)")
	}
	if res.Extra["repo_filename"] != "_copr:copr.fedorainfracloud.org:group_copr:integration_tests.repo" {
		t.Fatalf("repo_filename = %v (group sanitization)", res.Extra["repo_filename"])
	}
}

func TestModuleCoprBadName(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleCopr(context.Background(), conn, map[string]any{"name": "noSlash"})
	if err == nil {
		t.Fatal("want error for name without a slash")
	}
}
