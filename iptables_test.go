package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestIptablesRuleArgs(t *testing.T) {
	got := iptablesRuleArgs(map[string]any{
		"protocol": "tcp", "destination_port": "22", "jump": "ACCEPT",
	})
	want := " -p tcp --dport 22 -j ACCEPT"
	if got != want {
		t.Fatalf("iptablesRuleArgs = %q, want %q", got, want)
	}
	if got := iptablesRuleArgs(map[string]any{}); got != "" {
		t.Fatalf("iptablesRuleArgs(empty) = %q", got)
	}
}

func TestModuleIptablesPresentNew(t *testing.T) {
	ruleArgs := iptablesRuleArgs(map[string]any{"protocol": "tcp", "destination_port": "22", "jump": "ACCEPT"})
	conn := newFakeConn(map[string]remoteexec.Result{
		"iptables -C INPUT" + ruleArgs: {RC: 1},
		"iptables -A INPUT" + ruleArgs: {RC: 0},
	})
	res, err := moduleIptables(context.Background(), conn, map[string]any{
		"chain": "INPUT", "protocol": "tcp", "destination_port": "22", "jump": "ACCEPT",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleIptablesPresentAlreadyThere(t *testing.T) {
	ruleArgs := iptablesRuleArgs(map[string]any{"jump": "ACCEPT"})
	conn := newFakeConn(map[string]remoteexec.Result{
		"iptables -C INPUT" + ruleArgs: {RC: 0},
	})
	res, err := moduleIptables(context.Background(), conn, map[string]any{"chain": "INPUT", "jump": "ACCEPT"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleIptablesInsertAction(t *testing.T) {
	ruleArgs := iptablesRuleArgs(map[string]any{"jump": "DROP"})
	conn := newFakeConn(map[string]remoteexec.Result{
		"iptables -C INPUT" + ruleArgs: {RC: 1},
		"iptables -I INPUT" + ruleArgs: {RC: 0},
	})
	res, err := moduleIptables(context.Background(), conn, map[string]any{
		"chain": "INPUT", "jump": "DROP", "action": "insert",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleIptablesAbsent(t *testing.T) {
	ruleArgs := iptablesRuleArgs(map[string]any{"jump": "DROP"})
	conn := newFakeConn(map[string]remoteexec.Result{
		"iptables -C INPUT" + ruleArgs: {RC: 0},
		"iptables -D INPUT" + ruleArgs: {RC: 0},
	})
	res, err := moduleIptables(context.Background(), conn, map[string]any{
		"chain": "INPUT", "jump": "DROP", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleIptablesAbsentAlreadyGone(t *testing.T) {
	ruleArgs := iptablesRuleArgs(map[string]any{"jump": "DROP"})
	conn := newFakeConn(map[string]remoteexec.Result{
		"iptables -C INPUT" + ruleArgs: {RC: 1},
	})
	res, err := moduleIptables(context.Background(), conn, map[string]any{
		"chain": "INPUT", "jump": "DROP", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleIptablesTable(t *testing.T) {
	ruleArgs := iptablesRuleArgs(map[string]any{"jump": "MASQUERADE"})
	conn := newFakeConn(map[string]remoteexec.Result{
		"iptables -t nat -C POSTROUTING" + ruleArgs: {RC: 1},
		"iptables -t nat -A POSTROUTING" + ruleArgs: {RC: 0},
	})
	res, err := moduleIptables(context.Background(), conn, map[string]any{
		"chain": "POSTROUTING", "jump": "MASQUERADE", "table": "nat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleIptablesValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleIptables(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing chain")
	}
	if _, err := moduleIptables(context.Background(), conn, map[string]any{"chain": "INPUT", "state": "bogus"}); err == nil {
		t.Fatal("want error for invalid state")
	}
	if _, err := moduleIptables(context.Background(), conn, map[string]any{"chain": "INPUT", "action": "bogus"}); err == nil {
		t.Fatal("want error for invalid action")
	}
}
