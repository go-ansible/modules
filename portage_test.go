package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePortageInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"qlist -Ie sys-apps/foo >/dev/null 2>&1": {RC: 1},
		"emerge --noreplace --ask=n sys-apps/foo": {
			RC:     0,
			Stdout: ">>> Emerging (1 of 1) sys-apps/foo\n",
		},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"package": "sys-apps/foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModulePortageAlreadyInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"qlist -Ie sys-apps/foo >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"package": "sys-apps/foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no emerge run", conn.Commands)
	}
}

func TestModulePortageEmergeButNothingInstalled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"qlist -Ie sys-apps/foo >/dev/null 2>&1":  {RC: 1},
		"emerge --noreplace --ask=n sys-apps/foo": {RC: 0, Stdout: "Nothing to merge.\n"},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"package": "sys-apps/foo"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when emerge prints no Emerging line")
	}
}

func TestModulePortageNameAlias(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"qlist -Ie sys-apps/foo >/dev/null 2>&1": {RC: 1},
		"emerge --update --noreplace --ask=n sys-apps/foo": {
			RC:     0,
			Stdout: ">>> Emerging (1 of 1) sys-apps/foo\n",
		},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"name": "sys-apps/foo", "update": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePortageUnmerge(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"qlist -Ie sys-apps/foo >/dev/null 2>&1": {RC: 0},
		"emerge --unmerge --ask=n sys-apps/foo":  {RC: 0},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"package": "sys-apps/foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePortageUnmergeAlreadyAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"qlist -Ie sys-apps/foo >/dev/null 2>&1": {RC: 1},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"package": "sys-apps/foo", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePortageDepcleanNoPackage(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"emerge --depclean --ask=n": {RC: 0, Stdout: "Number removed: 3\n"},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"depclean": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePortageDepcleanNoneRemoved(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"emerge --depclean --ask=n": {RC: 0, Stdout: "Number removed: 0\n"},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"depclean": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePortageDepcleanWithPackageRequiresAbsentState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePortage(context.Background(), conn, map[string]any{
		"package": "sys-apps/foo", "depclean": true,
	}); err == nil {
		t.Fatal("want error: depclean+package needs an absent-family state")
	}
}

func TestModulePortageSyncOnly(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"emerge --sync --quiet --ask=n": {RC: 0},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"sync": "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: sync alone reports Ok")
	}
}

func TestModulePortageRequiredOneOf(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePortage(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error: one of package/sync/depclean is required")
	}
}

func TestModulePortageSetAtomAlwaysEmerges(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"emerge --noreplace --ask=n @world": {RC: 0, Stdout: ">>> Emerging (1 of 3) foo\n"},
	})
	res, err := modulePortage(context.Background(), conn, map[string]any{"package": "@world"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: a set atom is never treated as already-installed")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v, want no qlist probe for a set atom", conn.Commands)
	}
}
