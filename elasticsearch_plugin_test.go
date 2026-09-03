package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleElasticsearchPluginInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -f /usr/share/elasticsearch/bin/elasticsearch-plugin":              {RC: 0},
		"test -d /usr/share/elasticsearch/plugins/analysis-icu":                  {RC: 1},
		"/usr/share/elasticsearch/bin/elasticsearch-plugin install analysis-icu": {RC: 0, Stdout: "-> Installed analysis-icu\n"},
	})
	res, err := moduleElasticsearchPlugin(context.Background(), conn, map[string]any{"name": "analysis-icu"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleElasticsearchPluginAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -f /usr/share/elasticsearch/bin/elasticsearch-plugin": {RC: 0},
		"test -d /usr/share/elasticsearch/plugins/analysis-icu":     {RC: 0},
	})
	res, err := moduleElasticsearchPlugin(context.Background(), conn, map[string]any{"name": "analysis-icu"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleElasticsearchPluginRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -f /usr/share/elasticsearch/bin/elasticsearch-plugin":             {RC: 0},
		"test -d /usr/share/elasticsearch/plugins/analysis-icu":                 {RC: 0},
		"/usr/share/elasticsearch/bin/elasticsearch-plugin remove analysis-icu": {RC: 0},
	})
	res, err := moduleElasticsearchPlugin(context.Background(), conn, map[string]any{"name": "analysis-icu", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleElasticsearchPluginNoBinary(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -f /usr/share/elasticsearch/bin/elasticsearch-plugin": {RC: 1},
		"test -f /usr/share/elasticsearch/bin/plugin":               {RC: 1},
	})
	res, err := moduleElasticsearchPlugin(context.Background(), conn, map[string]any{"name": "analysis-icu"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when no plugin binary is found")
	}
}

func TestModuleElasticsearchPluginInstallFailureParsesError(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -f /usr/share/elasticsearch/bin/elasticsearch-plugin": {RC: 0},
		"test -d /usr/share/elasticsearch/plugins/analysis-icu":     {RC: 1},
		"/usr/share/elasticsearch/bin/elasticsearch-plugin install analysis-icu": {
			RC: 1, Stdout: "Some noise\nERROR: plugin already too new\n",
		},
	})
	res, err := moduleElasticsearchPlugin(context.Background(), conn, map[string]any{"name": "analysis-icu"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a non-zero install exit")
	}
	if res.Msg == "" {
		t.Fatal("want a parsed error message")
	}
}

func TestModuleElasticsearchPluginMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleElasticsearchPlugin(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleElasticsearchPluginSrcUrlMutuallyExclusive(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -f /usr/share/elasticsearch/bin/elasticsearch-plugin": {RC: 0},
	})
	if _, err := moduleElasticsearchPlugin(context.Background(), conn, map[string]any{
		"name": "analysis-icu", "src": "file:///tmp/x.zip", "url": "http://example.com/x.zip",
	}); err == nil {
		t.Fatal("want error for src+url both set")
	}
}
