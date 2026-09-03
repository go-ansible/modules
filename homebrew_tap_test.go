package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHomebrewTapPresentNew(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes": {RC: 1},
		"brew tap homebrew/dupes":                         {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleHomebrewTapPresentAlreadyTapped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes": {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no tap/trust commands attempted, commands = %v", conn.Commands)
	}
}

func TestModuleHomebrewTapAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes": {RC: 0},
		"brew untap homebrew/dupes":                       {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewTapAbsentNotTapped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes": {RC: 1},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHomebrewTapURLSingleName(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes":                   {RC: 1},
		"brew tap homebrew/dupes https://github.com/example/homebrew-dupes": {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{
		"name": "homebrew/dupes", "url": "https://github.com/example/homebrew-dupes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleHomebrewTapURLWithMultipleNamesRejected(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewTap(context.Background(), conn, map[string]any{
		"name": []any{"homebrew/dupes", "homebrew/cask"}, "url": "https://github.com/example/homebrew-dupes",
	}); err == nil {
		t.Fatal("want error: url requires exactly one name")
	}
}

func TestModuleHomebrewTapTrustNewlyTrusting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes":            {RC: 0},
		"brew trust --json v1 2>/dev/null | grep -qF homebrew/dupes": {RC: 1},
		"brew trust homebrew/dupes":                                  {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes", "trust": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if res.Msg != "updated trust" {
		t.Fatalf("msg = %q", res.Msg)
	}
	wantCmds := []string{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes",
		"brew trust --json v1 2>/dev/null | grep -qF homebrew/dupes",
		"brew trust homebrew/dupes",
	}
	if len(conn.Commands) != len(wantCmds) {
		t.Fatalf("commands = %v, want %v", conn.Commands, wantCmds)
	}
	for i, c := range wantCmds {
		if conn.Commands[i] != c {
			t.Fatalf("commands[%d] = %q, want %q", i, conn.Commands[i], c)
		}
	}
}

func TestModuleHomebrewTapTrustAlreadyTrusted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes":            {RC: 0},
		"brew trust --json v1 2>/dev/null | grep -qF homebrew/dupes": {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes", "trust": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("want unchanged (already trusted), res = %+v", res)
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("want no trust/untrust command run, commands = %v", conn.Commands)
	}
}

func TestModuleHomebrewTapTrustFalseUntrusting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes":            {RC: 0},
		"brew trust --json v1 2>/dev/null | grep -qF homebrew/dupes": {RC: 0},
		"brew untrust homebrew/dupes":                                {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes", "trust": false})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if res.Msg != "updated trust" {
		t.Fatalf("msg = %q", res.Msg)
	}
}

func TestModuleHomebrewTapTrustFalseAlreadyUntrusted(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes":            {RC: 0},
		"brew trust --json v1 2>/dev/null | grep -qF homebrew/dupes": {RC: 1},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes", "trust": false})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleHomebrewTapTrustTrueWithStateAbsentErrors(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewTap(context.Background(), conn, map[string]any{
		"name": "homebrew/dupes", "trust": true, "state": "absent",
	}); err == nil {
		t.Fatal("want error: combining trust=true with state=absent must be rejected")
	}
	if len(conn.Commands) != 0 {
		t.Fatalf("want no commands run before the validation error, commands = %v", conn.Commands)
	}
}

func TestModuleHomebrewTapTrustFalseWithStateAbsentAllowed(t *testing.T) {
	// Only trust=true + state=absent is documented as an error; trust=false
	// alongside state=absent should just run the ordinary untap flow and
	// never reach the trust-checking logic (state=="absent" short-circuits
	// it).
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes": {RC: 0},
		"brew untap homebrew/dupes":                       {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{
		"name": "homebrew/dupes", "trust": false, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 {
		t.Fatalf("want only the untap flow, no trust check, commands = %v", conn.Commands)
	}
}

func TestModuleHomebrewTapTrustUnsetLeavesTrustUntouched(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"brew tap 2>/dev/null | grep -qxF homebrew/dupes": {RC: 0},
	})
	res, err := moduleHomebrewTap(context.Background(), conn, map[string]any{"name": "homebrew/dupes"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no trust check when trust is unset, commands = %v", conn.Commands)
	}
}

func TestModuleHomebrewTapMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleHomebrewTap(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
