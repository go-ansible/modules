package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSnapAliasMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSnapAlias(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleSnapAliasPresentMissingAlias(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSnapAlias(context.Background(), conn, map[string]any{"name": "curl"}); err == nil {
		t.Fatal("want error: alias required when state is present")
	}
}

func TestModuleSnapAliasPresentNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap aliases 2>/dev/null | grep -qE " + shellQuote("^curl +curlx "): {RC: 1},
		"snap alias curl curlx": {RC: 0},
	})
	res, err := moduleSnapAlias(context.Background(), conn, map[string]any{"name": "curl", "alias": "curlx"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSnapAliasPresentAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap aliases 2>/dev/null | grep -qE " + shellQuote("^curl +curlx "): {RC: 0},
	})
	res, err := moduleSnapAlias(context.Background(), conn, map[string]any{"name": "curl", "alias": "curlx"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSnapAliasPresentMultipleOnlyMissingAdded(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap aliases 2>/dev/null | grep -qE " + shellQuote("^curl +a1 "): {RC: 0},
		"snap aliases 2>/dev/null | grep -qE " + shellQuote("^curl +a2 "): {RC: 1},
		"snap alias curl a2": {RC: 0},
	})
	res, err := moduleSnapAlias(context.Background(), conn, map[string]any{
		"name": "curl", "alias": []any{"a1", "a2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 3 {
		t.Fatalf("commands = %v, want 2 checks + 1 add (only a2)", conn.Commands)
	}
}

func TestModuleSnapAliasAbsentNoAliasRemovesAll(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap unalias curl": {RC: 0},
	})
	res, err := moduleSnapAlias(context.Background(), conn, map[string]any{"name": "curl", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Msg != "removed all aliases" {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestModuleSnapAliasAbsentSpecificPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap aliases 2>/dev/null | grep -qE " + shellQuote("^curl +curlx "): {RC: 0},
		"snap unalias curlx": {RC: 0},
	})
	res, err := moduleSnapAlias(context.Background(), conn, map[string]any{
		"name": "curl", "alias": "curlx", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSnapAliasAbsentSpecificNotPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap aliases 2>/dev/null | grep -qE " + shellQuote("^curl +curlx "): {RC: 1},
	})
	res, err := moduleSnapAlias(context.Background(), conn, map[string]any{
		"name": "curl", "alias": "curlx", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSnapAliasPluralAliasesKey(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"snap aliases 2>/dev/null | grep -qE " + shellQuote("^curl +curlx "): {RC: 1},
		"snap alias curl curlx": {RC: 0},
	})
	res, err := moduleSnapAlias(context.Background(), conn, map[string]any{
		"name": "curl", "aliases": []any{"curlx"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: aliases (plural) accepted when alias is absent")
	}
}

func TestModuleSnapAliasInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSnapAlias(context.Background(), conn, map[string]any{"name": "curl", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
