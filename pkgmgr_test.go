package modules

import (
	"context"
	"errors"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

// pkgmgrFakePresence backs a query/install/remove/latest closure set for
// direct unit tests of pkgManagerLoop, independent of any one module's
// shell-command composition (that's covered per-module in the other
// *_test.go files). The conn parameter each closure receives is unused
// here on purpose: these tests exercise pkgManagerLoop's own control
// flow, not command strings.
type pkgmgrFakePresence struct {
	present   map[string]bool
	installed []string
	removed   []string
	latested  []string
}

func (p *pkgmgrFakePresence) query(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	return p.present[name], nil
}

func (p *pkgmgrFakePresence) install(ctx context.Context, conn remoteexec.Connection, names []string) error {
	p.installed = append(p.installed, names...)
	return nil
}

func (p *pkgmgrFakePresence) remove(ctx context.Context, conn remoteexec.Connection, names []string) error {
	p.removed = append(p.removed, names...)
	return nil
}

func (p *pkgmgrFakePresence) latest(ctx context.Context, conn remoteexec.Connection, names []string) error {
	p.latested = append(p.latested, names...)
	return nil
}

func TestPkgManagerLoopPresentNothingToInstall(t *testing.T) {
	p := &pkgmgrFakePresence{present: map[string]bool{"a": true, "b": true}}
	conn := newFakeConn(nil)
	res, err := pkgManagerLoop(context.Background(), conn, []string{"a", "b"}, "present", p.query, p.install, p.remove, p.latest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(p.installed) != 0 {
		t.Fatalf("installed = %v, want none", p.installed)
	}
}

func TestPkgManagerLoopPresentSomeMissing(t *testing.T) {
	p := &pkgmgrFakePresence{present: map[string]bool{"a": true, "b": false}}
	conn := newFakeConn(nil)
	res, err := pkgManagerLoop(context.Background(), conn, []string{"a", "b"}, "present", p.query, p.install, p.remove, p.latest)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(p.installed) != 1 || p.installed[0] != "b" {
		t.Fatalf("installed = %v, want only b", p.installed)
	}
	if res.Msg != "b" {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestPkgManagerLoopInstalledAliasBehavesAsPresent(t *testing.T) {
	p := &pkgmgrFakePresence{present: map[string]bool{"a": false}}
	conn := newFakeConn(nil)
	res, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, "installed", p.query, p.install, p.remove, p.latest)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || len(p.installed) != 1 {
		t.Fatalf("res = %+v, installed = %v", res, p.installed)
	}
}

func TestPkgManagerLoopAbsent(t *testing.T) {
	p := &pkgmgrFakePresence{present: map[string]bool{"a": true, "b": false}}
	conn := newFakeConn(nil)
	res, err := pkgManagerLoop(context.Background(), conn, []string{"a", "b"}, "absent", p.query, p.install, p.remove, p.latest)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(p.removed) != 1 || p.removed[0] != "a" {
		t.Fatalf("removed = %v, want only a", p.removed)
	}
}

func TestPkgManagerLoopAbsentAliases(t *testing.T) {
	for _, alias := range []string{"absent", "removed", "uninstalled"} {
		p := &pkgmgrFakePresence{present: map[string]bool{"a": true}}
		conn := newFakeConn(nil)
		res, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, alias, p.query, p.install, p.remove, p.latest)
		if err != nil {
			t.Fatalf("%s: %v", alias, err)
		}
		if !res.Changed || len(p.removed) != 1 {
			t.Fatalf("%s: res = %+v, removed = %v", alias, res, p.removed)
		}
	}
}

func TestPkgManagerLoopAbsentNothingToRemove(t *testing.T) {
	p := &pkgmgrFakePresence{present: map[string]bool{"a": false}}
	conn := newFakeConn(nil)
	res, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, "absent", p.query, p.install, p.remove, p.latest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if res.Msg != "already absent" {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestPkgManagerLoopLatest(t *testing.T) {
	p := &pkgmgrFakePresence{present: map[string]bool{"a": true, "b": true}}
	conn := newFakeConn(nil)
	res, err := pkgManagerLoop(context.Background(), conn, []string{"a", "b"}, "latest", p.query, p.install, p.remove, p.latest)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: latest is always reported changed")
	}
	if len(p.latested) != 2 {
		t.Fatalf("latested = %v, want both names passed through with no query", p.latested)
	}
}

func TestPkgManagerLoopUpgradedAlias(t *testing.T) {
	p := &pkgmgrFakePresence{present: map[string]bool{"a": true}}
	conn := newFakeConn(nil)
	res, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, "upgraded", p.query, p.install, p.remove, p.latest)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || len(p.latested) != 1 {
		t.Fatalf("res = %+v, latested = %v", res, p.latested)
	}
}

func TestPkgManagerLoopLatestNilFuncErrors(t *testing.T) {
	p := &pkgmgrFakePresence{}
	conn := newFakeConn(nil)
	if _, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, "latest", p.query, p.install, p.remove, nil); err == nil {
		t.Fatal("want error: state=latest unsupported when latest func is nil")
	}
	if _, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, "upgraded", p.query, p.install, p.remove, nil); err == nil {
		t.Fatal("want error for the upgraded alias too")
	}
}

func TestPkgManagerLoopQueryErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	query := func(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
		return false, wantErr
	}
	conn := newFakeConn(nil)
	if _, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, "present", query, nil, nil, nil); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestPkgManagerLoopInstallErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	p := &pkgmgrFakePresence{present: map[string]bool{"a": false}}
	install := func(ctx context.Context, conn remoteexec.Connection, names []string) error { return wantErr }
	conn := newFakeConn(nil)
	if _, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, "present", p.query, install, nil, nil); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestPkgManagerLoopRemoveErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	p := &pkgmgrFakePresence{present: map[string]bool{"a": true}}
	remove := func(ctx context.Context, conn remoteexec.Connection, names []string) error { return wantErr }
	conn := newFakeConn(nil)
	if _, err := pkgManagerLoop(context.Background(), conn, []string{"a"}, "absent", p.query, nil, remove, nil); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestResolveNamesSingleString(t *testing.T) {
	names, err := resolveNames(map[string]any{"name": "curl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "curl" {
		t.Fatalf("names = %v", names)
	}
}

func TestResolveNamesList(t *testing.T) {
	names, err := resolveNames(map[string]any{"name": []any{"curl", "git"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "curl" || names[1] != "git" {
		t.Fatalf("names = %v", names)
	}
}

func TestResolveNamesMissing(t *testing.T) {
	if _, err := resolveNames(map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestResolveNamesWrongType(t *testing.T) {
	if _, err := resolveNames(map[string]any{"name": 42}); err == nil {
		t.Fatal("want error: name must be a string or list, got int")
	}
}

func TestResolveNamesEmptyStringIsNotRejected(t *testing.T) {
	// Surprising edge case: argStringList's `case string` branch returns
	// a 1-element slice even for "", so resolveNames's `len(names) > 0`
	// check accepts it before ever reaching requireString's own
	// empty-string rejection. A caller passing name: "" therefore gets
	// []string{""} rather than an error — flagged for the orchestrating
	// session, not fixed here.
	names, err := resolveNames(map[string]any{"name": ""})
	if err != nil {
		t.Fatalf("got err %v; documenting current (surprising) behavior: no error", err)
	}
	if len(names) != 1 || names[0] != "" {
		t.Fatalf("names = %v, want [\"\"] per current behavior", names)
	}
}

func TestPkgManagerLoopMsgJoinsNames(t *testing.T) {
	p := &pkgmgrFakePresence{present: map[string]bool{"a": false, "b": false}}
	conn := newFakeConn(nil)
	res, err := pkgManagerLoop(context.Background(), conn, []string{"a", "b"}, "present", p.query, p.install, p.remove, p.latest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Msg != strings.Join([]string{"a", "b"}, ", ") {
		t.Fatalf("msg = %q", res.Msg)
	}
}
