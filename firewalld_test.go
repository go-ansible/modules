package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleFirewalldEnableServicePermanentImmediate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --permanent --zone=public --query-service=https": {RC: 1},
		"firewall-cmd --permanent --zone=public --add-service=https":   {RC: 0},
		"firewall-cmd --zone=public --query-service=https":             {RC: 1},
		"firewall-cmd --zone=public --add-service=https":               {RC: 0},
	})
	res, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"service": "https", "state": "enabled", "permanent": true, "immediate": true, "zone": "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleFirewalldAlreadyEnabled(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --permanent --zone=public --query-service=https": {RC: 0},
	})
	res, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"service": "https", "state": "enabled", "permanent": true, "zone": "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleFirewalldDisablePort(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --zone=public --query-port=8081/tcp":  {RC: 0},
		"firewall-cmd --zone=public --remove-port=8081/tcp": {RC: 0},
	})
	res, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"port": "8081/tcp", "state": "disabled", "zone": "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleFirewalldDefaultZone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --get-default-zone":               {RC: 0, Stdout: "public"},
		"firewall-cmd --zone=public --query-masquerade": {RC: 1},
		"firewall-cmd --zone=public --add-masquerade":   {RC: 0},
	})
	res, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"masquerade": true, "state": "enabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleFirewalldZoneCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --get-default-zone":          {RC: 0, Stdout: "public"},
		"firewall-cmd --permanent --get-zones":     {RC: 0, Stdout: "public work home"},
		"firewall-cmd --permanent --new-zone=dmz2": {RC: 0},
	})
	res, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"zone": "dmz2", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleFirewalldZoneAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"firewall-cmd --permanent --get-zones": {RC: 0, Stdout: "public work home"},
	})
	res, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"zone": "public", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleFirewalldValidation(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleFirewalld(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing state")
	}
	if _, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"service": "https", "port": "80/tcp", "state": "enabled",
	}); err == nil {
		t.Fatal("want error for mutually exclusive targets")
	}
	if _, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"zone": "public", "state": "enabled",
	}); err == nil {
		t.Fatal("want error: enabled/disabled needs a target")
	}
	if _, err := moduleFirewalld(context.Background(), conn, map[string]any{
		"service": "https", "state": "present",
	}); err == nil {
		t.Fatal("want error: present/absent needs no target")
	}
}
