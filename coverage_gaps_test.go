package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModulePipAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip3 show requests >/dev/null 2>&1": {RC: 0},
		"pip3 uninstall -y requests":         {RC: 0},
	})
	res, err := modulePip(context.Background(), conn, map[string]any{"name": "requests", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModulePipAbsentAlreadyGone(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip3 show requests >/dev/null 2>&1": {RC: 1},
	})
	res, err := modulePip(context.Background(), conn, map[string]any{"name": "requests", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePipMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := modulePip(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModulePipCustomExecutable(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"pip show requests >/dev/null 2>&1": {RC: 0},
	})
	res, err := modulePip(context.Background(), conn, map[string]any{"name": "requests", "executable": "pip"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleUserModifyExisting(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd bob":        {RC: 0},
		"usermod -s /bin/bash bob": {RC: 0},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{"name": "bob", "shell": "/bin/bash"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleUserSystemNoCreateHome(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent passwd svc": {RC: 2},
	})
	res, err := moduleUser(context.Background(), conn, map[string]any{
		"name": "svc", "system": true, "create_home": false, "home": "/opt/svc",
		"groups": []any{"a", "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	want := "useradd -M -r -d /opt/svc -G a,b svc"
	if conn.Commands[1] != want {
		t.Fatalf("cmd = %q, want %q", conn.Commands[1], want)
	}
}

func TestModuleTemplateNoVarsDefaultsEmpty(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "t.j2")
	if err := os.WriteFile(src, []byte("static text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.txt")
	conn := local()
	res, err := moduleTemplate(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleTemplateMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "t.j2")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.txt")
	conn := local()
	res, err := moduleTemplate(context.Background(), conn, map[string]any{"src": src, "dest": dest, "mode": "0600"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestModuleTemplateMissingArgs(t *testing.T) {
	conn := local()
	if _, err := moduleTemplate(context.Background(), conn, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error for missing src")
	}
	if _, err := moduleTemplate(context.Background(), conn, map[string]any{"src": mustTempTemplate(t)}); err == nil {
		t.Fatal("want error for missing dest")
	}
}

func TestModuleAptLatest(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"DEBIAN_FRONTEND=noninteractive apt-get install -y -q --only-upgrade curl": {RC: 0},
	})
	res, err := moduleApt(context.Background(), conn, map[string]any{"name": "curl", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleAptUpdateCache(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"DEBIAN_FRONTEND=noninteractive apt-get update -q":         {RC: 0},
		"dpkg -s curl 2>/dev/null | grep -q '^Status:.*installed'": {RC: 0},
	})
	res, err := moduleApt(context.Background(), conn, map[string]any{"name": "curl", "update_cache": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already installed")
	}
	if conn.Commands[0] != "DEBIAN_FRONTEND=noninteractive apt-get update -q" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleAptMultiplePackagesMixed(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"dpkg -s a 2>/dev/null | grep -q '^Status:.*installed'":  {RC: 0},
		"dpkg -s b 2>/dev/null | grep -q '^Status:.*installed'":  {RC: 1},
		"DEBIAN_FRONTEND=noninteractive apt-get install -y -q b": {RC: 0},
	})
	res, err := moduleApt(context.Background(), conn, map[string]any{"name": []any{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSystemdEnableAlready(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl is-enabled nginx": {RC: 0},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"name": "nginx", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSystemdStopAlreadyStopped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl is-active nginx": {RC: 3},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"name": "nginx", "state": "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleSystemdRestartReload(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"systemctl restart nginx": {RC: 0},
	})
	res, err := moduleSystemd(context.Background(), conn, map[string]any{"name": "nginx", "state": "restarted"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleCronAbsentViaFakeConn(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"crontab -l 2>/dev/null": {RC: 0, Stdout: "# ansible: job1\n* * * * * echo hi\n"},
		"crontab -":              {RC: 0},
	})
	res, err := moduleCron(context.Background(), conn, map[string]any{"name": "job1", "state": "absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleCronWithUser(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"crontab -u alice -l 2>/dev/null": {RC: 1},
	})
	res, err := moduleCron(context.Background(), conn, map[string]any{"name": "job1", "job": "echo hi", "user": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[1] != "crontab -u alice -" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleCronWriteFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"crontab -l 2>/dev/null": {RC: 1},
		"crontab -":              {RC: 1, Stderr: "denied"},
	})
	res, err := moduleCron(context.Background(), conn, map[string]any{"name": "job1", "job": "echo hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}

func TestModuleGroupSystem(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"getent group svc": {RC: 2},
	})
	res, err := moduleGroup(context.Background(), conn, map[string]any{"name": "svc", "system": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[1] != "groupadd -r svc" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleGroupMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGroup(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleGitVersionCheckout(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin")
	initGitRepo(t, origin)
	dest := filepath.Join(t.TempDir(), "clone")
	conn := local()
	if _, err := moduleGit(context.Background(), conn, map[string]any{"repo": origin, "dest": dest}); err != nil {
		t.Fatal(err)
	}
	// Re-run pinned to "main" explicitly (not HEAD) — exercises the
	// non-HEAD checkout-target branch.
	res, err := moduleGit(context.Background(), conn, map[string]any{"repo": origin, "dest": dest, "version": "main"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: already on main at the same commit")
	}
}

func TestModuleCopyModeAlreadyCorrect(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(dest, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleCopy(context.Background(), conn, map[string]any{"content": "x", "dest": dest, "mode": "0600"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged: content and mode both already correct")
	}
}

func TestModuleFileOwnerGroup(t *testing.T) {
	// chown to a group every CI runner has (the process's own primary
	// group, via getgid) — chown 1) itself always succeeds against a
	// group the caller belongs to, unlike an arbitrary user, and 2)
	// exercises the owner/group branch without needing root.
	f := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(f, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleFile(context.Background(), conn, map[string]any{"path": f, "group": currentGroupName(t)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func currentGroupName(t *testing.T) string {
	t.Helper()
	conn := local()
	out, err := run(context.Background(), conn, "id -gn")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSkipByCreatesRemovesBothUnset(t *testing.T) {
	conn := local()
	skip, _, err := skipByCreatesRemoves(context.Background(), conn, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Fatal("want no skip when neither creates nor removes is set")
	}
}

func TestSkipByCreatesRemovesRemovesExists(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(f, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	skip, _, err := skipByCreatesRemoves(context.Background(), conn, map[string]any{"removes": f})
	if err != nil {
		t.Fatal(err)
	}
	if skip {
		t.Fatal("want no skip: removes path still exists")
	}
}

func TestTokenizeBackslashEscape(t *testing.T) {
	got := tokenize(`a\ b c`)
	want := []string{"a b", "c"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("tokenize = %v", got)
	}
}

func TestShellQuoteSafeAndUnsafe(t *testing.T) {
	if got := shellQuote("plain-path_1.txt"); got != "plain-path_1.txt" {
		t.Fatalf("safe path got requoted: %q", got)
	}
	if got := shellQuote("has space"); got != "'has space'" {
		t.Fatalf("unsafe path not quoted: %q", got)
	}
	if got := shellQuote("it's"); got != `'it'"'"'s'` {
		t.Fatalf("embedded quote not escaped: %q", got)
	}
}

func TestSplitLinesEmpty(t *testing.T) {
	if got := splitLines(""); got != nil {
		t.Fatalf("splitLines(\"\") = %v, want nil", got)
	}
	if got := splitLines("\n"); got != nil {
		t.Fatalf("splitLines(\"\\n\") = %v, want nil", got)
	}
}

func TestShellQuoteEmpty(t *testing.T) {
	if got := shellQuote(""); got != "''" {
		t.Fatalf("shellQuote(\"\") = %q, want ''", got)
	}
}

func TestWriteRemotePutFailure(t *testing.T) {
	conn := &failAfterConn{n: 0}
	err := writeRemote(context.Background(), conn, "/x", []byte("y"))
	if err == nil {
		t.Fatal("want error when Put fails")
	}
}
