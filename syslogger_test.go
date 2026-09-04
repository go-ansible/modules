package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSyslogger(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"logger -p daemon.info -t ansible_syslogger -- 'I will end up as daemon.info'": {RC: 0},
	})
	res, err := moduleSyslogger(context.Background(), conn, map[string]any{"msg": "I will end up as daemon.info"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
	if res.Extra["facility"] != "daemon" || res.Extra["priority"] != "info" {
		t.Fatalf("extra = %+v", res.Extra)
	}
}

func TestModuleSyslogerWithLogPidAndCustomFacility(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"logger -p user.err -t ansible_syslogger -i -- 'Hello from Ansible'": {RC: 0},
	})
	res, err := moduleSyslogger(context.Background(), conn, map[string]any{
		"msg": "Hello from Ansible", "priority": "err", "facility": "user", "log_pid": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSyslogerCustomIdent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"logger -p daemon.alert -t MyApp -- 'I want to believe'": {RC: 0},
	})
	res, err := moduleSyslogger(context.Background(), conn, map[string]any{
		"ident": "MyApp", "msg": "I want to believe", "priority": "alert",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["ident"] != "MyApp" {
		t.Fatalf("extra = %+v", res.Extra)
	}
}

func TestModuleSyslogerFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"logger -p daemon.info -t ansible_syslogger -- hi": {RC: 1, Stderr: "logger: command not found-ish"},
	})
	res, err := moduleSyslogger(context.Background(), conn, map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed")
	}
}

func TestModuleSyslogerInvalidPriority(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSyslogger(context.Background(), conn, map[string]any{"msg": "hi", "priority": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleSyslogerMissingMsg(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSyslogger(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error")
	}
}
