package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestGetURLCmd(t *testing.T) {
	cmd := getURLCmd("/opt/f", "https://example.com/f")
	want := "if command -v curl >/dev/null 2>&1; then curl -fsSL -o /opt/f https://example.com/f" +
		"; elif command -v wget >/dev/null 2>&1; then wget -q -O /opt/f https://example.com/f" +
		"; else echo 'get_url: neither curl nor wget found' >&2; exit 127; fi"
	if cmd != want {
		t.Fatalf("cmd = %q, want %q", cmd, want)
	}
}

func TestModuleGetURLDownloadsWhenMissing(t *testing.T) {
	dest := "/opt/f"
	url := "https://example.com/f"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 1},
		getURLCmd(dest, url):          {RC: 0},
	})
	res, err := moduleGetURL(context.Background(), conn, map[string]any{"url": url, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleGetURLSkipsWhenPresent(t *testing.T) {
	dest := "/opt/f"
	url := "https://example.com/f"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 0},
	})
	res, err := moduleGetURL(context.Background(), conn, map[string]any{"url": url, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when dest already exists")
	}
	if len(conn.Commands) != 1 {
		t.Fatalf("want no download attempted, commands = %v", conn.Commands)
	}
}

func TestModuleGetURLForceRedownloads(t *testing.T) {
	dest := "/opt/f"
	url := "https://example.com/f"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 0},
		getURLCmd(dest, url):          {RC: 0},
	})
	res, err := moduleGetURL(context.Background(), conn, map[string]any{"url": url, "dest": dest, "force": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed when force is set")
	}
}

func TestModuleGetURLDownloadFails(t *testing.T) {
	dest := "/opt/f"
	url := "https://example.com/f"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 1},
		getURLCmd(dest, url):          {RC: 22, Stderr: "curl: (22) HTTP 404"},
	})
	res, err := moduleGetURL(context.Background(), conn, map[string]any{"url": url, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when download fails")
	}
}

func TestModuleGetURLMode(t *testing.T) {
	dest := "/opt/f"
	url := "https://example.com/f"
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e " + shellQuote(dest): {RC: 1},
		getURLCmd(dest, url):          {RC: 0},
		"stat -c '%s|%a|%F' " + dest + " 2>/dev/null || stat -f '%z|%Lp|%HT' " + dest + " 2>/dev/null": {
			RC: 0, Stdout: "10|644|regular file\n",
		},
		"chmod 0755 " + dest: {RC: 0},
	})
	res, err := moduleGetURL(context.Background(), conn, map[string]any{"url": url, "dest": dest, "mode": "0755"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed for a mode change")
	}
}

func TestModuleGetURLMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGetURL(context.Background(), conn, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error for missing url")
	}
	if _, err := moduleGetURL(context.Background(), conn, map[string]any{"url": "https://x"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}

func TestModuleGetURLBadMode(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGetURL(context.Background(), conn, map[string]any{
		"url": "https://x", "dest": "/x", "mode": "not-octal",
	}); err == nil {
		t.Fatal("want error for invalid mode")
	}
}
