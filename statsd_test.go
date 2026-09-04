package modules

import (
	"context"
	"strings"
	"testing"
)

func TestModuleStatsdCounter(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleStatsd(context.Background(), conn, map[string]any{
		"metric": "my_counter", "metric_type": "counter", "value": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("commands = %v", conn.Commands)
	}
	cmd := conn.Commands[0]
	for _, want := range []string{"bash -c", "/dev/udp/localhost/8125", "my_counter:1|c"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("cmd %q missing %q", cmd, want)
		}
	}
}

func TestModuleStatsdGaugeTCP(t *testing.T) {
	conn := newFakeConn(nil)
	res, err := moduleStatsd(context.Background(), conn, map[string]any{
		"metric": "my_gauge", "metric_type": "gauge", "value": 7,
		"host": "10.0.0.1", "port": 9125, "protocol": "tcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	cmd := conn.Commands[0]
	for _, want := range []string{"/dev/tcp/10.0.0.1/9125", "my_gauge:7|g"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("cmd %q missing %q", cmd, want)
		}
	}
}

func TestModuleStatsdGaugeDeltaPositive(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleStatsd(context.Background(), conn, map[string]any{
		"metric": "g", "metric_type": "gauge", "value": 5, "delta": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.Commands[0], "g:+5|g") {
		t.Fatalf("cmd = %q, want explicit + sign for a positive delta", conn.Commands[0])
	}
}

func TestModuleStatsdMetricPrefix(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleStatsd(context.Background(), conn, map[string]any{
		"metric": "requests", "metric_type": "counter", "value": 1, "metric_prefix": "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.Commands[0], "app.requests:1|c") {
		t.Fatalf("cmd = %q, want prefixed bucket with a dot", conn.Commands[0])
	}
}

func TestModuleStatsdMissingMetricType(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleStatsd(context.Background(), conn, map[string]any{"metric": "x", "value": 1}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleStatsdMissingValue(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleStatsd(context.Background(), conn, map[string]any{"metric": "x", "metric_type": "counter"}); err == nil {
		t.Fatal("want error")
	}
}
