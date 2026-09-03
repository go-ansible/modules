package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleKibanaPluginInstallModern(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /opt/kibana/bin/kibana --version":                                        {RC: 0, Stdout: "7.10.0\n"},
		"test -d /opt/kibana/installedPlugins/marvel":                                                 {RC: 1},
		"LANGUAGE=C LC_ALL=C /opt/kibana/bin/kibana-plugin install elasticsearch/marvel --timeout 1m": {RC: 0},
	})
	res, err := moduleKibanaPlugin(context.Background(), conn, map[string]any{"name": "elasticsearch/marvel"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKibanaPluginAlreadyPresentSkips(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /opt/kibana/bin/kibana --version": {RC: 0, Stdout: "7.10.0\n"},
		"test -d /opt/kibana/installedPlugins/marvel":          {RC: 0},
	})
	res, err := moduleKibanaPlugin(context.Background(), conn, map[string]any{"name": "elasticsearch/marvel"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKibanaPluginRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /opt/kibana/bin/kibana --version":                          {RC: 0, Stdout: "7.10.0\n"},
		"test -d /opt/kibana/installedPlugins/marvel":                                   {RC: 0},
		"LANGUAGE=C LC_ALL=C /opt/kibana/bin/kibana-plugin remove elasticsearch/marvel": {RC: 0},
	})
	res, err := moduleKibanaPlugin(context.Background(), conn, map[string]any{
		"name": "elasticsearch/marvel", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKibanaPluginVersionProbeFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /opt/kibana/bin/kibana --version": {RC: 1, Stderr: "no such file"},
	})
	res, err := moduleKibanaPlugin(context.Background(), conn, map[string]any{"name": "elasticsearch/marvel"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the version probe fails")
	}
}

func TestModuleKibanaPluginLegacyVersionUsesPluginSubcommand(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /opt/kibana/bin/kibana --version":                                          {RC: 0, Stdout: "4.5.0\n"},
		"test -d /opt/kibana/installedPlugins/marvel":                                                   {RC: 1},
		"LANGUAGE=C LC_ALL=C /opt/kibana/bin/kibana plugin --install elasticsearch/marvel --timeout 1m": {RC: 0},
	})
	res, err := moduleKibanaPlugin(context.Background(), conn, map[string]any{"name": "elasticsearch/marvel"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleKibanaPluginMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleKibanaPlugin(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestLooseVersionGreater(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"7.10.0", "4.6", true},
		{"4.5.0", "4.6", false},
		{"4.6.0", "4.6", true},
		{"4.6", "4.6", false},
	}
	for _, c := range cases {
		if got := looseVersionGreater(c.a, c.b); got != c.want {
			t.Errorf("looseVersionGreater(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
