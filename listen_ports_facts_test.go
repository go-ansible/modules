package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleListenPortsFactsNetstat(t *testing.T) {
	netstatOut := "Active Internet connections (only servers)\n" +
		"Proto Recv-Q Send-Q Local Address           Foreign Address         State       PID/Program name\n" +
		"tcp        0      0 0.0.0.0:22              0.0.0.0:*               LISTEN      596/sshd\n" +
		"tcp6       0      0 :::80                   :::*                    LISTEN      -\n" +
		"udp        0      0 0.0.0.0:68              0.0.0.0:*                           123/dhclient\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":                           {RC: 0, Stdout: "Linux\n"},
		"command -v netstat >/dev/null 2>&1": {RC: 0},
		"netstat -p -l -u -n -t":             {RC: 0, Stdout: netstatOut},
		"ps -o lstart -p 596":                {RC: 0, Stdout: "STARTED\nThu Feb  2 13:29:45 2017\n"},
		"ps -o user -p 596":                  {RC: 0, Stdout: "USER\nroot\n"},
		"ps -o lstart -p 0":                  {RC: 1},
		"ps -o user -p 0":                    {RC: 1},
		"ps -o lstart -p 123":                {RC: 0, Stdout: "STARTED\nThu Feb  2 13:00:00 2017\n"},
		"ps -o user -p 123":                  {RC: 0, Stdout: "USER\nroot\n"},
	})
	res, err := moduleListenPortsFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	tcp := res.Extra["tcp_listen"].([]map[string]any)
	udp := res.Extra["udp_listen"].([]map[string]any)
	if len(tcp) != 2 || len(udp) != 1 {
		t.Fatalf("tcp = %#v\nudp = %#v", tcp, udp)
	}
	if tcp[0]["port"] != 22 || tcp[0]["pid"] != 596 || tcp[0]["name"] != "sshd" || tcp[0]["user"] != "root" {
		t.Fatalf("tcp[0] = %#v", tcp[0])
	}
	if _, has := tcp[0]["state"]; has {
		t.Fatalf("state should be pruned by default: %#v", tcp[0])
	}
	if tcp[1]["pid"] != 0 || tcp[1]["name"] != "" {
		t.Fatalf("tcp[1] (no permission) = %#v", tcp[1])
	}
	if udp[0]["port"] != 68 || udp[0]["protocol"] != "udp" {
		t.Fatalf("udp[0] = %#v", udp[0])
	}
}

func TestModuleListenPortsFactsIncludeNonListening(t *testing.T) {
	netstatOut := "tcp        0      0 10.0.0.1:443            10.80.0.1:5555          ESTABLISHED 42/nginx\n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":                           {RC: 0, Stdout: "Linux\n"},
		"command -v netstat >/dev/null 2>&1": {RC: 0},
		"netstat -p -u -n -t -a":             {RC: 0, Stdout: netstatOut},
		"ps -o lstart -p 42":                 {RC: 0, Stdout: "STARTED\ndate\n"},
		"ps -o user -p 42":                   {RC: 0, Stdout: "USER\nwww-data\n"},
	})
	res, err := moduleListenPortsFacts(context.Background(), conn, map[string]any{"include_non_listening": true})
	if err != nil {
		t.Fatal(err)
	}
	tcp := res.Extra["tcp_listen"].([]map[string]any)
	if len(tcp) != 1 {
		t.Fatalf("tcp = %#v", tcp)
	}
	if tcp[0]["state"] != "ESTABLISHED" || tcp[0]["foreign_address"] != "10.80.0.1:5555" {
		t.Fatalf("tcp[0] = %#v", tcp[0])
	}
}

func TestModuleListenPortsFactsSS(t *testing.T) {
	ssOut := "Netid  State   Recv-Q  Send-Q   Local Address:Port   Peer Address:Port  Process\n" +
		"tcp    LISTEN  0       128      0.0.0.0:22           0.0.0.0:*          users:((\"sshd\",pid=596,fd=3))\n" +
		"tcp    LISTEN  0       128      [::]:80              [::]:*             \n"
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":                           {RC: 0, Stdout: "Linux\n"},
		"command -v netstat >/dev/null 2>&1": {RC: 1},
		"command -v ss >/dev/null 2>&1":      {RC: 0},
		"ss -p -l -u -n -t":                  {RC: 0, Stdout: ssOut},
		"ps -o lstart -p 596":                {RC: 0, Stdout: "STARTED\ndate\n"},
		"ps -o user -p 596":                  {RC: 0, Stdout: "USER\nroot\n"},
		"ps -o lstart -p 0":                  {RC: 1},
		"ps -o user -p 0":                    {RC: 1},
	})
	res, err := moduleListenPortsFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	tcp := res.Extra["tcp_listen"].([]map[string]any)
	if len(tcp) != 2 {
		t.Fatalf("tcp = %#v", tcp)
	}
	if tcp[0]["pid"] != 596 || tcp[0]["name"] != "sshd" || tcp[0]["port"] != 22 {
		t.Fatalf("tcp[0] = %#v", tcp[0])
	}
	if tcp[1]["pid"] != 0 || tcp[1]["port"] != 80 {
		t.Fatalf("tcp[1] = %#v", tcp[1])
	}
}

func TestModuleListenPortsFactsNonLinux(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s": {RC: 0, Stdout: "Darwin\n"},
	})
	res, err := moduleListenPortsFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed on non-Linux")
	}
}

func TestModuleListenPortsFactsNoCommandFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"uname -s":                           {RC: 0, Stdout: "Linux\n"},
		"command -v netstat >/dev/null 2>&1": {RC: 1},
		"command -v ss >/dev/null 2>&1":      {RC: 1},
	})
	res, err := moduleListenPortsFacts(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed when neither command is found")
	}
}

func TestListenPortsSplitFieldsN(t *testing.T) {
	got := listenPortsSplitFieldsN(`a  b   c d e f g h`, 7)
	want := []string{"a", "b", "c", "d", "e", "f", "g h"}
	if len(got) != len(want) {
		t.Fatalf("got = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
