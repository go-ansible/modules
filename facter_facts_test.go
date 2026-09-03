package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleFacterFactsGathers(t *testing.T) {
	getCmd := "env LANGUAGE=C LC_ALL=C facter --json"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v facter": {RC: 0},
		getCmd:              {RC: 0, Stdout: `{"os":{"family":"RedHat"},"timezone":"UTC"}`},
	})
	res, err := moduleFacterFacts(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	facter, ok := res.Facts["facter"].(map[string]any)
	if !ok {
		t.Fatalf("Facts[facter] = %#v, want a map", res.Facts["facter"])
	}
	if facter["timezone"] != "UTC" {
		t.Fatalf("facter[timezone] = %v", facter["timezone"])
	}
}

func TestModuleFacterFactsWithArguments(t *testing.T) {
	getCmd := "env LANGUAGE=C LC_ALL=C facter --json -p system_uptime timezone"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v facter": {RC: 0},
		getCmd:              {RC: 0, Stdout: `{"timezone":"UTC"}`},
	})
	res, err := moduleFacterFacts(context.Background(), fc, map[string]any{
		"arguments": []any{"-p", "system_uptime", "timezone"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleFacterFactsFallbackPath(t *testing.T) {
	testCmd := "test -e /opt/puppetlabs/bin/facter"
	getCmd := "env LANGUAGE=C LC_ALL=C /opt/puppetlabs/bin/facter --json"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v facter": {RC: 1},
		testCmd:             {RC: 0},
		getCmd:              {RC: 0, Stdout: `{}`},
	})
	res, err := moduleFacterFacts(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleFacterFactsNoBinary(t *testing.T) {
	testCmd := "test -e /opt/puppetlabs/bin/facter"
	fc := newFakeConn(map[string]remoteexec.Result{
		"command -v facter": {RC: 1},
		testCmd:             {RC: 1},
	})
	res, err := moduleFacterFacts(context.Background(), fc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("res = %+v", res)
	}
}
