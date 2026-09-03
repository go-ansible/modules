package modules

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

const homectlActiveShow = "ActiveState=active\n"

func TestModuleHomectlServiceNotActive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl show systemd-homed.service -p ActiveState": {RC: 0, Stdout: "ActiveState=inactive\n"},
	})
	res, err := moduleHomectl(context.Background(), conn, map[string]any{"name": "alice", "password": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed when homed service is not active")
	}
}

func TestModuleHomectlCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl show systemd-homed.service -p ActiveState": {RC: 0, Stdout: homectlActiveShow},
		"homectl inspect alice -j --no-pager":                 {RC: 1},
		"homectl create --identity=-":                         {RC: 0},
	})
	res, err := moduleHomectl(context.Background(), conn, map[string]any{
		"name": "alice", "password": "s3cret!", "shell": "/bin/zsh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	// verify the create record's stdin content
	var createStdin string
	for i, c := range conn.Commands {
		if c == "homectl create --identity=-" {
			createStdin = conn.Stdins[i]
		}
	}
	if createStdin == "" {
		t.Fatal("expected a create call with stdin")
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(createStdin), &record); err != nil {
		t.Fatalf("invalid JSON record: %v (%s)", err, createStdin)
	}
	if record["userName"] != "alice" {
		t.Fatalf("record = %#v", record)
	}
	secret, ok := record["secret"].(map[string]any)
	if !ok || secret["password"].([]any)[0] != "s3cret!" {
		t.Fatalf("secret = %#v", record["secret"])
	}
}

func TestModuleHomectlAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl show systemd-homed.service -p ActiveState": {RC: 0, Stdout: homectlActiveShow},
		"homectl inspect bob -j --no-pager":                   {RC: 1},
	})
	res, err := moduleHomectl(context.Background(), conn, map[string]any{"name": "bob", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHomectlRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl show systemd-homed.service -p ActiveState": {RC: 0, Stdout: homectlActiveShow},
		"homectl inspect bob -j --no-pager":                   {RC: 0, Stdout: `{"userName":"bob"}`},
		"homectl remove bob":                                  {RC: 0},
	})
	res, err := moduleHomectl(context.Background(), conn, map[string]any{"name": "bob", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleHomectlModifyChangedField(t *testing.T) {
	existing := `{"userName":"alice","realName":"Alice Old","binding":{"x":1},"signature":[1],"status":{"y":2}}`
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl show systemd-homed.service -p ActiveState": {RC: 0, Stdout: homectlActiveShow},
		"homectl inspect alice -j --no-pager":                 {RC: 0, Stdout: existing},
		"homectl update alice --identity=-":                   {RC: 0},
	})
	res, err := moduleHomectl(context.Background(), conn, map[string]any{
		"name": "alice", "password": "x", "realname": "Alice New",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	var updateStdin string
	for i, c := range conn.Commands {
		if c == "homectl update alice --identity=-" {
			updateStdin = conn.Stdins[i]
		}
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(updateStdin), &record); err != nil {
		t.Fatal(err)
	}
	if record["realName"] != "Alice New" {
		t.Fatalf("record = %#v", record)
	}
	if _, has := record["signature"]; has {
		t.Fatal("signature should be stripped from the update record")
	}
}

func TestModuleHomectlModifyIdempotent(t *testing.T) {
	existing := `{"userName":"alice","realName":"Alice"}`
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl show systemd-homed.service -p ActiveState": {RC: 0, Stdout: homectlActiveShow},
		"homectl inspect alice -j --no-pager":                 {RC: 0, Stdout: existing},
	})
	res, err := moduleHomectl(context.Background(), conn, map[string]any{
		"name": "alice", "password": "x", "realname": "Alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged: realname already matches", res)
	}
	for _, c := range conn.Commands {
		if c == "homectl update alice --identity=-" {
			t.Fatal("should not have run update when nothing changed")
		}
	}
}

func TestModuleHomectlLockedFalseIsNoOp(t *testing.T) {
	// Verbatim-preserved upstream bug: locked=false can never take
	// effect, since real homectl's own check is a Python truthiness
	// test on the bool.
	existing := `{"userName":"alice","locked":true}`
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl show systemd-homed.service -p ActiveState": {RC: 0, Stdout: homectlActiveShow},
		"homectl inspect alice -j --no-pager":                 {RC: 0, Stdout: existing},
	})
	res, err := moduleHomectl(context.Background(), conn, map[string]any{
		"name": "alice", "password": "x", "locked": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: locked=false must be a no-op (verbatim upstream bug)")
	}
}

func TestModuleHomectlLanguageAlwaysReportsChanged(t *testing.T) {
	// Verbatim-preserved upstream bug: language change-detection
	// compares against the `locked` argument, so it never reports
	// unchanged even when the value already matches.
	existing := `{"userName":"alice","preferredLanguage":"en_US"}`
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl show systemd-homed.service -p ActiveState": {RC: 0, Stdout: homectlActiveShow},
		"homectl inspect alice -j --no-pager":                 {RC: 0, Stdout: existing},
		"homectl update alice --identity=-":                   {RC: 0},
	})
	res, err := moduleHomectl(context.Background(), conn, map[string]any{
		"name": "alice", "password": "x", "language": "en_US",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed: verbatim-preserved upstream bug always reports changed for language")
	}
}

func TestModuleHomectlMissingPassword(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleHomectl(context.Background(), conn, map[string]any{"name": "alice"})
	if err == nil {
		t.Fatal("want error when password missing for state=present")
	}
}

func TestModuleHomectlResizeRequiresDisksize(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleHomectl(context.Background(), conn, map[string]any{
		"name": "alice", "password": "x", "resize": true,
	})
	if err == nil {
		t.Fatal("want error when resize given without disksize")
	}
}

func TestHomectlRandomSalt(t *testing.T) {
	s := homectlRandomSalt(16)
	if len(s) != 16 {
		t.Fatalf("len = %d", len(s))
	}
	for _, r := range s {
		if !strings.ContainsRune(homectlCryptAlphabet, r) {
			t.Fatalf("salt contains invalid char %q", r)
		}
	}
}
