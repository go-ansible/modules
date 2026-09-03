package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestCronvarVarName(t *testing.T) {
	cases := []struct {
		line   string
		want   string
		wantOK bool
	}{
		{"EMAIL=doug@example.com", "EMAIL", true},
		{"# a comment", "", false},
		{"", "", false},
		{"* * * * * echo hi", "", false},
		{"LOGFILE=/var/log/x.log", "LOGFILE", true},
	}
	for _, c := range cases {
		got, ok := cronvarVarName(c.line)
		if ok != c.wantOK || got != c.want {
			t.Errorf("cronvarVarName(%q) = (%q,%v), want (%q,%v)", c.line, got, ok, c.want, c.wantOK)
		}
	}
}

func TestCronvarApplyEntryAppendsNew(t *testing.T) {
	out, changed := cronvarApplyEntry(nil, "EMAIL", "doug@example.com", "present", "", "")
	if !changed || len(out) != 1 || out[0] != "EMAIL=doug@example.com" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestCronvarApplyEntryReplacesExisting(t *testing.T) {
	existing := []string{"EMAIL=old@example.com", "* * * * * job"}
	out, changed := cronvarApplyEntry(existing, "EMAIL", "new@example.com", "present", "", "")
	if !changed || out[0] != "EMAIL=new@example.com" || out[1] != "* * * * * job" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestCronvarApplyEntryInsertAfter(t *testing.T) {
	existing := []string{"PATH=/bin", "* * * * * job"}
	out, changed := cronvarApplyEntry(existing, "EMAIL", "doug@example.com", "present", "PATH", "")
	if !changed || len(out) != 3 || out[1] != "EMAIL=doug@example.com" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestCronvarApplyEntryInsertBefore(t *testing.T) {
	existing := []string{"PATH=/bin"}
	out, changed := cronvarApplyEntry(existing, "EMAIL", "doug@example.com", "present", "", "PATH")
	if !changed || len(out) != 2 || out[0] != "EMAIL=doug@example.com" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestCronvarApplyEntryAbsent(t *testing.T) {
	existing := []string{"LEGACY=x", "PATH=/bin"}
	out, changed := cronvarApplyEntry(existing, "LEGACY", "", "absent", "", "")
	if !changed || len(out) != 1 || out[0] != "PATH=/bin" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestCronvarApplyEntryAbsentNotFound(t *testing.T) {
	existing := []string{"PATH=/bin"}
	out, changed := cronvarApplyEntry(existing, "LEGACY", "", "absent", "", "")
	if changed || len(out) != 1 {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestModuleCronvarPresentNoCrontabYet(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"crontab -u root -l 2>/dev/null": {RC: 1},
	})
	res, err := moduleCronvar(context.Background(), conn, map[string]any{
		"name": "EMAIL", "value": "doug@ansibmod.con.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	last := conn.Commands[len(conn.Commands)-1]
	if last != "crontab -u root -" {
		t.Fatalf("last command = %q", last)
	}
	if conn.Stdins[len(conn.Stdins)-1] != "EMAIL=doug@ansibmod.con.com\n" {
		t.Fatalf("stdin = %q", conn.Stdins[len(conn.Stdins)-1])
	}
}

func TestModuleCronvarAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"crontab -u root -l 2>/dev/null": {RC: 0, Stdout: "LEGACY=x\n* * * * * job\n"},
	})
	res, err := moduleCronvar(context.Background(), conn, map[string]any{
		"name": "LEGACY", "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	last := conn.Stdins[len(conn.Stdins)-1]
	if last != "* * * * * job\n" {
		t.Fatalf("stdin = %q", last)
	}
}

func TestModuleCronvarCustomUser(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"crontab -u deploy -l 2>/dev/null": {RC: 1},
	})
	res, err := moduleCronvar(context.Background(), conn, map[string]any{
		"name": "EMAIL", "value": "x@example.com", "user": "deploy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if conn.Commands[len(conn.Commands)-1] != "crontab -u deploy -" {
		t.Fatalf("commands = %v", conn.Commands)
	}
}

func TestModuleCronvarCronFile(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"cat /etc/cron.d/ansible_yum-autoupdate 2>/dev/null": {RC: 0, Stdout: "PATH=/bin\n"},
	})
	res, err := moduleCronvar(context.Background(), conn, map[string]any{
		"name": "LOGFILE", "value": "/var/log/yum-autoupdate.log", "cron_file": "ansible_yum-autoupdate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleCronvarBackup(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"crontab -u root -l 2>/dev/null": {RC: 0, Stdout: "PATH=/bin\n"},
		"date +%Y%m%d%H%M%S":             {RC: 0, Stdout: "20260101120000"},
	})
	res, err := moduleCronvar(context.Background(), conn, map[string]any{
		"name": "EMAIL", "value": "x@example.com", "backup": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	backup, _ := res.Extra["backup"].(string)
	if !strings.HasPrefix(backup, "/tmp/cronvar-backup-EMAIL-20260101120000") {
		t.Fatalf("backup = %q", backup)
	}
}

func TestModuleCronvarMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleCronvar(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
	if _, err := moduleCronvar(context.Background(), conn, map[string]any{"name": "EMAIL"}); err == nil {
		t.Fatal("want error for missing value when state=present")
	}
}

func TestModuleCronvarInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleCronvar(context.Background(), conn, map[string]any{
		"name": "EMAIL", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
