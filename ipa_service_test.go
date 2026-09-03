package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleIpaServiceCreate(t *testing.T) {
	showCmd := "ipa service-show http/host01.example.com --all --raw"
	addCmd := "ipa service-add http/host01.example.com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 2},
		addCmd:           {RC: 0},
	})
	res, err := moduleIpaService(context.Background(), fc, map[string]any{"krbcanonicalname": "http/host01.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaServiceAddHosts(t *testing.T) {
	showCmd := "ipa service-show http/host01.example.com --all --raw"
	addHostCmd := "ipa service-add-host http/host01.example.com --host=host01.example.com --host=host02.example.com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  krbcanonicalname: http/host01.example.com\n"},
		addHostCmd:       {RC: 0},
	})
	res, err := moduleIpaService(context.Background(), fc, map[string]any{
		"krbcanonicalname": "http/host01.example.com",
		"hosts":            []any{"host01.example.com", "host02.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaServiceDelete(t *testing.T) {
	showCmd := "ipa service-show http/host01.example.com --all --raw"
	delCmd := "ipa service-del http/host01.example.com"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v ipa": {RC: 0},
		showCmd:          {RC: 0, Stdout: "  krbcanonicalname: http/host01.example.com\n"},
		delCmd:           {RC: 0},
	})
	res, err := moduleIpaService(context.Background(), fc, map[string]any{
		"krbcanonicalname": "http/host01.example.com", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleIpaServiceMissingName(t *testing.T) {
	fc := newFakeConn(nil)
	if _, err := moduleIpaService(context.Background(), fc, map[string]any{}); err == nil {
		t.Fatal("want error for missing krbcanonicalname")
	}
}
