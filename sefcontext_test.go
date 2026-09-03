package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const sefcontextListSample = `/srv/git_repos(/.*)?                              all files       system_u:object_r:httpd_sys_content_t:s0
/srv/containers = /var/lib/containers
`

func TestModuleSefcontextAddType(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage fcontext -C -l": {RC: 0, Stdout: sefcontextListSample},
		"semanage fcontext -a -f a -t httpd_sys_rw_content_t '/srv/newpath(/.*)?'": {RC: 0},
	})
	res, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/srv/newpath(/.*)?", "setype": "httpd_sys_rw_content_t", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSefcontextTypeAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage fcontext -C -l": {RC: 0, Stdout: sefcontextListSample},
	})
	res, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/srv/git_repos(/.*)?", "setype": "httpd_sys_content_t", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSefcontextModifyType(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage fcontext -C -l": {RC: 0, Stdout: sefcontextListSample},
		"semanage fcontext -m -f a -t httpd_sys_rw_content_t '/srv/git_repos(/.*)?'": {RC: 0},
	})
	res, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/srv/git_repos(/.*)?", "setype": "httpd_sys_rw_content_t", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSefcontextEquivalenceAdd(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage fcontext -C -l":                           {RC: 0, Stdout: sefcontextListSample},
		"semanage fcontext -a -e /var/lib/other /srv/other": {RC: 0},
	})
	res, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/srv/other", "substitute": "/var/lib/other", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSefcontextEquivalenceAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage fcontext -C -l": {RC: 0, Stdout: sefcontextListSample},
	})
	res, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/srv/containers", "substitute": "/var/lib/containers", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSefcontextDeleteType(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage fcontext -C -l": {RC: 0, Stdout: sefcontextListSample},
		"semanage fcontext -d -f a -t httpd_sys_content_t '/srv/git_repos(/.*)?'": {RC: 0},
	})
	res, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/srv/git_repos(/.*)?", "setype": "httpd_sys_content_t", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSefcontextDeleteBothWhenNeitherGiven(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"semanage fcontext -C -l":                                     {RC: 0, Stdout: sefcontextListSample},
		"semanage fcontext -d -e /var/lib/containers /srv/containers": {RC: 0},
	})
	res, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/srv/containers", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSefcontextValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSefcontext(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing target")
	}
	if _, err := moduleSefcontext(context.Background(), conn, map[string]any{"target": "/x"}); err == nil {
		t.Fatal("want error: setype or substitute required when present")
	}
	if _, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/x", "setype": "a_t", "substitute": "/y",
	}); err == nil {
		t.Fatal("want error: setype and substitute mutually exclusive")
	}
	if _, err := moduleSefcontext(context.Background(), conn, map[string]any{
		"target": "/x", "setype": "a_t", "ftype": "bogus",
	}); err == nil {
		t.Fatal("want error for bad ftype")
	}
}
