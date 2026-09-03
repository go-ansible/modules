package modules

import (
	"context"
	"io"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func ufwBaseFakeConn() map[string]remoteexec.Result {
	return map[string]remoteexec.Result{
		"command -v ufw >/dev/null 2>&1 && command -v grep >/dev/null 2>&1": {RC: 0},
	}
}

func TestModuleUfwEnableChanged(t *testing.T) {
	conn := &statusTogglingConn{
		statusReplies: []string{"Status: inactive\n", "Status: active\n"},
		on: map[string]remoteexec.Result{
			"command -v ufw >/dev/null 2>&1 && command -v grep >/dev/null 2>&1": {RC: 0},
			"grep -h '^### tuple' /lib/ufw/user.rules /lib/ufw/user6.rules /etc/ufw/user.rules " +
				"/etc/ufw/user6.rules /var/lib/ufw/user.rules /var/lib/ufw/user6.rules 2>/dev/null": {RC: 0, Stdout: ""},
			"ufw -f enable": {RC: 0, Stdout: "Firewall is active and enabled on system startup\n"},
		},
	}
	res, err := moduleUfw(context.Background(), conn, map[string]any{"state": "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

// statusTogglingConn is a minimal remoteexec.Connection that returns
// successive entries from statusReplies for "ufw status verbose" (pre-
// then post-), simulating a real firewall state change between the two
// status queries — something fakeConn's single fixed-response-per-
// command map can't represent.
type statusTogglingConn struct {
	on            map[string]remoteexec.Result
	statusReplies []string
	statusCalls   int
}

func (s *statusTogglingConn) Exec(ctx context.Context, cmd string, stdin io.Reader) (remoteexec.Result, error) {
	if cmd == "ufw status verbose" {
		i := s.statusCalls
		s.statusCalls++
		if i < len(s.statusReplies) {
			return remoteexec.Result{RC: 0, Stdout: s.statusReplies[i]}, nil
		}
	}
	if res, ok := s.on[cmd]; ok {
		return res, nil
	}
	return remoteexec.Result{}, nil
}

func (s *statusTogglingConn) Put(ctx context.Context, localPath, remotePath string, opts remoteexec.PutOptions) error {
	return nil
}
func (s *statusTogglingConn) Fetch(ctx context.Context, remotePath, localPath string) error {
	return nil
}
func (s *statusTogglingConn) Remove(ctx context.Context, remotePath string) error { return nil }
func (s *statusTogglingConn) TempPath(base string) string                         { return "/tmp/" + base }
func (s *statusTogglingConn) Close() error                                        { return nil }

var _ remoteexec.Connection = (*statusTogglingConn)(nil)

func TestModuleUfwLoggingIdempotentWhenTextUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ufw >/dev/null 2>&1 && command -v grep >/dev/null 2>&1": {RC: 0},
		"ufw status verbose": {RC: 0, Stdout: "Status: active\nLogging: on (low)\n"},
		"ufw logging on":     {RC: 0, Stdout: "Logging enabled\n"},
		"grep -h '^### tuple' /lib/ufw/user.rules /lib/ufw/user6.rules /etc/ufw/user.rules " +
			"/etc/ufw/user6.rules /var/lib/ufw/user.rules /var/lib/ufw/user6.rules 2>/dev/null": {RC: 0, Stdout: ""},
	})
	res, err := moduleUfw(context.Background(), conn, map[string]any{"logging": "on"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("res = %+v, want unchanged since status text is identical before/after", res)
	}
}

func TestModuleUfwRuleBasic(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ufw >/dev/null 2>&1 && command -v grep >/dev/null 2>&1": {RC: 0},
		"ufw status verbose": {RC: 0, Stdout: "Status: active\n"},
		"grep -h '^### tuple' /lib/ufw/user.rules /lib/ufw/user6.rules /etc/ufw/user.rules " +
			"/etc/ufw/user6.rules /var/lib/ufw/user.rules /var/lib/ufw/user6.rules 2>/dev/null": {RC: 0, Stdout: ""},
		"ufw allow from any to any port 80 proto tcp": {RC: 0, Stdout: "Rule added\n"},
	})
	res, err := moduleUfw(context.Background(), conn, map[string]any{
		"rule": "allow", "to_port": "80", "proto": "tcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "ufw allow from any to any port 80 proto tcp" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleUfwRuleWithInterfaceRequiresDirection(t *testing.T) {
	conn := newFakeConn(ufwBaseFakeConn())
	_, err := moduleUfw(context.Background(), conn, map[string]any{
		"rule": "allow", "interface": "eth0",
	})
	if err == nil {
		t.Fatal("want error when interface given without direction")
	}
}

func TestModuleUfwRuleInterfaceInOutRequiresRoute(t *testing.T) {
	conn := newFakeConn(ufwBaseFakeConn())
	_, err := moduleUfw(context.Background(), conn, map[string]any{
		"rule": "allow", "interface_in": "eth0", "interface_out": "eth1",
	})
	if err == nil {
		t.Fatal("want error when interface_in+interface_out given without route")
	}
}

func TestModuleUfwFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v ufw >/dev/null 2>&1 && command -v grep >/dev/null 2>&1": {RC: 0},
		"ufw status verbose": {RC: 0, Stdout: "Status: active\n"},
		"grep -h '^### tuple' /lib/ufw/user.rules /lib/ufw/user6.rules /etc/ufw/user.rules " +
			"/etc/ufw/user6.rules /var/lib/ufw/user.rules /var/lib/ufw/user6.rules 2>/dev/null": {RC: 0, Stdout: ""},
		"ufw logging bogus": {RC: 1, Stderr: "ERROR: Invalid logging level\n"},
	})
	res, err := moduleUfw(context.Background(), conn, map[string]any{"logging": "bogus"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed on non-zero ufw exit")
	}
}

func TestModuleUfwMissingCommand(t *testing.T) {
	conn := newFakeConn(ufwBaseFakeConn())
	_, err := moduleUfw(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error when no command given")
	}
}

func TestUfwInsertPositionZero(t *testing.T) {
	conn := newFakeConn(nil)
	pos, omit, err := ufwInsertPosition(context.Background(), conn, 3, "zero")
	if err != nil {
		t.Fatal(err)
	}
	if omit || pos != 3 {
		t.Fatalf("pos = %d, omit = %v", pos, omit)
	}
}

func TestUfwInsertPositionLastIPv4(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ufw status numbered": {RC: 0, Stdout: "[ 1] 22/tcp ALLOW IN Anywhere\n[ 2] 80/tcp ALLOW IN Anywhere (v6)\n"},
	})
	pos, omit, err := ufwInsertPosition(context.Background(), conn, 0, "last-ipv4")
	if err != nil {
		t.Fatal(err)
	}
	if omit || pos != 1 {
		t.Fatalf("pos = %d, omit = %v", pos, omit)
	}
}

func TestUfwVersionParse(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"ufw --version": {RC: 0, Stdout: "ufw 0.36\nCopyright...\n"},
	})
	major, minor, _, err := ufwVersion(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	if major != 0 || minor != 36 {
		t.Fatalf("version = %d.%d", major, minor)
	}
}
