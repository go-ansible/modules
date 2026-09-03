package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaHostCreate(t *testing.T) {
	showCmd := "ipa host-show host01.example.com --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
	})
	// Script the add command loosely since flag order depends on map
	// iteration in ipaHostScalarCreateFlags/ipaHostListCreateFlags for
	// the list part but scalars are ordered by ipaHostScalarSpecs.
	addCmd := "ipa host-add host01.example.com --description=Example host --ip-address=192.168.0.123"
	fc.on[addCmd] = remoteexec.Result{RC: 0}
	res, err := moduleIpaHost(context.Background(), fc, map[string]any{
		"fqdn": "host01.example.com", "description": "Example host", "ip_address": "192.168.0.123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, commands = %v", res, fc.Commands)
	}
	found := false
	for _, c := range fc.Commands {
		if strings.HasPrefix(c, "ipa host-add host01.example.com") && strings.Contains(c, "--ip-address=192.168.0.123") {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want an ip-address host-add call", fc.Commands)
	}
}

func TestModuleIpaHostRandomPassword(t *testing.T) {
	showCmd := "ipa host-show host02.example.com --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
	})
	res, err := moduleIpaHost(context.Background(), fc, map[string]any{
		"fqdn": "host02.example.com", "random_password": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range fc.Commands {
		if c == "ipa host-add host02.example.com --random" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want --random (not --random-password)", fc.Commands)
	}
}

func TestModuleIpaHostAbsentUpdateDNS(t *testing.T) {
	showCmd := "ipa host-show host01.example.com --all --raw"
	delCmd := "ipa host-del host01.example.com --updatedns"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  fqdn: host01.example.com\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaHost(context.Background(), fc, map[string]any{
		"fqdn": "host01.example.com", "state": "absent", "update_dns": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaHostForceCreationFalseSkipsCreate(t *testing.T) {
	showCmd := "ipa host-show host03.example.com --all --raw"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
	})
	res, err := moduleIpaHost(context.Background(), fc, map[string]any{
		"fqdn": "host03.example.com", "state": "disabled", "force_creation": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged (force_creation=false, absent, state!=present)", res)
	}
}

func TestModuleIpaHostMissingFQDN(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaHost(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing fqdn")
	}
}
