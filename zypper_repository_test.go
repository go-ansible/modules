package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleZypperRepositoryAddNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --quiet --non-interactive --xmlout repos": {RC: 6},
		"zypper --quiet --non-interactive addrepo --check --name nvidia-repo --gpgcheck --refresh ftp://download.nvidia.com/opensuse/12.2 nvidia-repo": {RC: 0},
	})
	res, err := moduleZypperRepository(context.Background(), conn, map[string]any{
		"name": "nvidia-repo", "repo": "ftp://download.nvidia.com/opensuse/12.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleZypperRepositoryAlreadyPresentUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --quiet --non-interactive --xmlout repos": {RC: 0, Stdout: zypperReposXML},
	})
	res, err := moduleZypperRepository(context.Background(), conn, map[string]any{
		"name": "repo-oss", "repo": "http://download.opensuse.org/tumbleweed/repo/oss/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged, res = %+v", res)
	}
}

func TestModuleZypperRepositoryChangedURL(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --quiet --non-interactive --xmlout repos":                                                                            {RC: 0, Stdout: zypperReposXML},
		"zypper --quiet --non-interactive removerepo repo-oss":                                                                       {RC: 0},
		"zypper --quiet --non-interactive addrepo --check --name repo-oss --gpgcheck --refresh http://new.example.com/oss/ repo-oss": {RC: 0},
	})
	res, err := moduleZypperRepository(context.Background(), conn, map[string]any{
		"name": "repo-oss", "repo": "http://new.example.com/oss/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZypperRepositoryRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --quiet --non-interactive --xmlout repos":      {RC: 0, Stdout: zypperReposXML},
		"zypper --quiet --non-interactive removerepo repo-oss": {RC: 0},
	})
	res, err := moduleZypperRepository(context.Background(), conn, map[string]any{
		"name": "repo-oss", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleZypperRepositoryRemoveNotPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --quiet --non-interactive --xmlout repos": {RC: 6},
	})
	res, err := moduleZypperRepository(context.Background(), conn, map[string]any{
		"name": "nope", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleZypperRepositoryWildcardRefresh(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"zypper --quiet --non-interactive refresh --force": {RC: 0},
	})
	res, err := moduleZypperRepository(context.Background(), conn, map[string]any{
		"repo": "*", "runrefresh": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged (runrefresh-only is reported unchanged, matching real zypper_repository)")
	}
	if res.Extra["runrefresh"] != true {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleZypperRepositoryWildcardWithoutRunrefreshErrors(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZypperRepository(context.Background(), conn, map[string]any{"repo": "*"}); err == nil {
		t.Fatal("want error: repo=* requires runrefresh")
	}
}

func TestModuleZypperRepositoryPresentRequiresRepo(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZypperRepository(context.Background(), conn, map[string]any{"name": "foo"}); err == nil {
		t.Fatal("want error: state=present requires repo")
	}
}

func TestModuleZypperRepositoryPresentRequiresName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZypperRepository(context.Background(), conn, map[string]any{"repo": "http://example.com/repo"}); err == nil {
		t.Fatal("want error: name required (no .repo-file support)")
	}
}

func TestModuleZypperRepositoryAbsentRequiresNameOrRepo(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZypperRepository(context.Background(), conn, map[string]any{"state": "absent"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleZypperRepositoryInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleZypperRepository(context.Background(), conn, map[string]any{
		"repo": "http://example.com/repo", "name": "foo", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
