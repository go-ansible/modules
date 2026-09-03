package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleLogstashPluginInstall(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin list logstash-input-beats":    {RC: 1},
		"LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin install logstash-input-beats": {RC: 0, Stdout: "Installation successful\n"},
	})
	res, err := moduleLogstashPlugin(context.Background(), conn, map[string]any{"name": "logstash-input-beats"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleLogstashPluginInstallWithVersion(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin list logstash-input-syslog":                    {RC: 1},
		"LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin install --version 3.2.0 logstash-input-syslog": {RC: 0},
	})
	res, err := moduleLogstashPlugin(context.Background(), conn, map[string]any{
		"name": "logstash-input-syslog", "version": "3.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleLogstashPluginAlreadyPresentSkips(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin list logstash-input-beats": {RC: 0},
	})
	res, err := moduleLogstashPlugin(context.Background(), conn, map[string]any{"name": "logstash-input-beats"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleLogstashPluginRemove(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin list logstash-filter-multiline":   {RC: 0},
		"LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin remove logstash-filter-multiline": {RC: 0},
	})
	res, err := moduleLogstashPlugin(context.Background(), conn, map[string]any{
		"name": "logstash-filter-multiline", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleLogstashPluginProxy(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin list logstash-input-beats":                                                                   {RC: 1},
		"http_proxy=http://myproxy:8080 https_proxy=http://myproxy:8080 LANGUAGE=C LC_ALL=C /usr/share/logstash/bin/logstash-plugin install logstash-input-beats": {RC: 0},
	})
	res, err := moduleLogstashPlugin(context.Background(), conn, map[string]any{
		"name": "logstash-input-beats", "proxy_host": "myproxy", "proxy_port": "8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleLogstashPluginMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLogstashPlugin(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
