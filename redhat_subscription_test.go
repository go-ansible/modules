package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleRedhatSubscriptionRegister(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 1},
		"subscription-manager register --username joe_user --password somepass": {RC: 0},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{
		"username": "joe_user", "password": "somepass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRedhatSubscriptionAlreadyRegistered(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 0},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{
		"username": "joe_user", "password": "somepass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRedhatSubscriptionMissingCredentials(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 1},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for missing username/activationkey/token")
	}
}

func TestModuleRedhatSubscriptionActivationkeyRequiresOrgID(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u": {RC: 0, Stdout: "0\n"},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{"activationkey": "key1"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for activationkey without org_id")
	}
}

func TestModuleRedhatSubscriptionUnregister(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                           {RC: 0, Stdout: "0\n"},
		"subscription-manager identity":   {RC: 0},
		"subscription-manager unregister": {RC: 0},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{"state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleRedhatSubscriptionUnregisterAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 1},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{"state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleRedhatSubscriptionNotRoot(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u": {RC: 0, Stdout: "1000\n"},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when not root")
	}
}

func TestModuleRedhatSubscriptionActivationKeyRegister(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 1},
		"subscription-manager register --org myorg --activationkey key1": {RC: 0},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{
		"activationkey": "key1", "org_id": "myorg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRedhatSubscriptionConfigureServer(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 1},
		"subscription-manager config --server.hostname=" + shellQuote("sat.example.com"): {RC: 0},
		"subscription-manager register --username joe_user --password somepass":          {RC: 0},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{
		"username": "joe_user", "password": "somepass", "server_hostname": "sat.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	// Confirm config ran BEFORE register.
	var configIdx, registerIdx = -1, -1
	for i, c := range conn.Commands {
		if c == "subscription-manager config --server.hostname="+shellQuote("sat.example.com") {
			configIdx = i
		}
		if c == "subscription-manager register --username joe_user --password somepass" {
			registerIdx = i
		}
	}
	if configIdx == -1 || registerIdx == -1 || configIdx > registerIdx {
		t.Fatalf("commands = %v, want config before register", conn.Commands)
	}
}

func TestModuleRedhatSubscriptionPoolIDs(t *testing.T) {
	available := "Product Name: Awesome OS Server\n" +
		"Pool ID:      0123456789abcdef0123456789abcdef\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 1},
		"subscription-manager register --username joe_user --password somepass":                {RC: 0},
		"subscription-manager list --available":                                                {RC: 0, Stdout: available},
		"subscription-manager attach --pool " + shellQuote("0123456789abcdef0123456789abcdef"): {RC: 0},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{
		"username": "joe_user", "password": "somepass",
		"pool_ids": []any{"0123456789abcdef0123456789abcdef"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleRedhatSubscriptionSyspurpose(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 0},
		"cat /etc/rhsm/syspurpose/syspurpose.json": {RC: 1}, // file does not exist yet
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{
		"syspurpose": map[string]any{"usage": "Production"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed for a new syspurpose attribute")
	}
	if len(conn.Stdins) == 0 {
		t.Fatal("want syspurpose.json written via stdin")
	}
	found := false
	for _, s := range conn.Stdins {
		if s != "" {
			found = true
			if !strings.Contains(s, "Production") {
				t.Fatalf("syspurpose content = %q, want it to mention Production", s)
			}
		}
	}
	if !found {
		t.Fatal("want at least one non-empty stdin (the syspurpose.json write)")
	}
}

func TestModuleRedhatSubscriptionSyspurposeUnknownAttribute(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"id -u":                         {RC: 0, Stdout: "0\n"},
		"subscription-manager identity": {RC: 0},
		"cat /etc/rhsm/syspurpose/syspurpose.json": {RC: 1},
	})
	res, err := moduleRedhatSubscription(context.Background(), conn, map[string]any{
		"syspurpose": map[string]any{"bogus_attr": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an unrecognized syspurpose attribute")
	}
}
