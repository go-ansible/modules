package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func launchdPlist(runAtLoad, keepAlive bool) string {
	if runAtLoad && keepAlive {
		return `{"RunAtLoad":true,"KeepAlive":true}`
	}
	if runAtLoad {
		return `{"RunAtLoad":true}`
	}
	if keepAlive {
		return `{"KeepAlive":true}`
	}
	return `{}`
}

func TestModuleLaunchdStartUnloaded(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"launchctl list":      {RC: 0, Stdout: ""},
		`printf '%s' "$HOME"`: {RC: 0, Stdout: "/Users/tester"},
		"test -e /Users/tester/Library/LaunchAgents/org.memcached.plist":                   {RC: 0},
		"plutil -convert json -o - /Users/tester/Library/LaunchAgents/org.memcached.plist": {RC: 0, Stdout: launchdPlist(false, false)},
		"launchctl load /Users/tester/Library/LaunchAgents/org.memcached.plist":            {RC: 0},
		"launchctl start org.memcached":                                                    {RC: 0},
	})
	res, err := moduleLaunchd(context.Background(), conn, map[string]any{
		"name": "org.memcached", "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	status := res.Extra["status"].(map[string]any)
	if status["previous_state"] != "unloaded" {
		t.Fatalf("status = %+v", status)
	}
}

func TestModuleLaunchdStopAlreadyStopped(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"launchctl list":      {RC: 0, Stdout: "-\t0\torg.memcached\n"},
		`printf '%s' "$HOME"`: {RC: 0, Stdout: "/Users/tester"},
		"test -e /Users/tester/Library/LaunchAgents/org.memcached.plist":                   {RC: 0},
		"plutil -convert json -o - /Users/tester/Library/LaunchAgents/org.memcached.plist": {RC: 0, Stdout: launchdPlist(false, false)},
	})
	res, err := moduleLaunchd(context.Background(), conn, map[string]any{
		"name": "org.memcached", "state": "stopped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleLaunchdStartedRunning(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"launchctl list":      {RC: 0, Stdout: "1234\t0\torg.memcached\n"},
		`printf '%s' "$HOME"`: {RC: 0, Stdout: "/Users/tester"},
		"test -e /Users/tester/Library/LaunchAgents/org.memcached.plist":                   {RC: 0},
		"plutil -convert json -o - /Users/tester/Library/LaunchAgents/org.memcached.plist": {RC: 0, Stdout: launchdPlist(false, false)},
	})
	res, err := moduleLaunchd(context.Background(), conn, map[string]any{
		"name": "org.memcached", "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	status := res.Extra["status"].(map[string]any)
	if status["current_pid"] != "1234" {
		t.Fatalf("status = %+v", status)
	}
}

func TestModuleLaunchdPlistNotFound(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"launchctl list":      {RC: 0, Stdout: ""},
		`printf '%s' "$HOME"`: {RC: 0, Stdout: "/Users/tester"},
		"test -e /Users/tester/Library/LaunchAgents/org.missing.plist": {RC: 1},
		"test -e /Library/LaunchAgents/org.missing.plist":              {RC: 1},
		"test -e /Library/LaunchDaemons/org.missing.plist":             {RC: 1},
		"test -e /System/Library/LaunchAgents/org.missing.plist":       {RC: 1},
		"test -e /System/Library/LaunchDaemons/org.missing.plist":      {RC: 1},
	})
	res, err := moduleLaunchd(context.Background(), conn, map[string]any{
		"name": "org.missing", "state": "started",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when the plist file can't be found")
	}
}

func TestModuleLaunchdEnabledRewritesRunAtLoad(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"launchctl list":      {RC: 0, Stdout: ""},
		`printf '%s' "$HOME"`: {RC: 0, Stdout: "/Users/tester"},
		"test -e /Users/tester/Library/LaunchAgents/org.memcached.plist":                   {RC: 0},
		"plutil -convert json -o - /Users/tester/Library/LaunchAgents/org.memcached.plist": {RC: 0, Stdout: launchdPlist(false, false)},
		"plutil -convert xml1 -o /Users/tester/Library/LaunchAgents/org.memcached.plist -": {RC: 0},
	})
	res, err := moduleLaunchd(context.Background(), conn, map[string]any{
		"name": "org.memcached", "enabled": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
}

func TestModuleLaunchdRequiresStateOrEnabled(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLaunchd(context.Background(), conn, map[string]any{"name": "org.memcached"}); err == nil {
		t.Fatal("want error when neither state nor enabled is given")
	}
}

func TestModuleLaunchdUnloadedAlways(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"launchctl list":      {RC: 0, Stdout: "1234\t0\torg.memcached\n"},
		`printf '%s' "$HOME"`: {RC: 0, Stdout: "/Users/tester"},
		"test -e /Users/tester/Library/LaunchAgents/org.memcached.plist":                   {RC: 0},
		"plutil -convert json -o - /Users/tester/Library/LaunchAgents/org.memcached.plist": {RC: 0, Stdout: launchdPlist(false, false)},
		"launchctl unload /Users/tester/Library/LaunchAgents/org.memcached.plist":          {RC: 0},
	})
	res, err := moduleLaunchd(context.Background(), conn, map[string]any{
		"name": "org.memcached", "state": "unloaded",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The fakeConn is stateless (its scripted `launchctl list` output
	// doesn't reflect the unload that just ran), so this only checks
	// that the module actually ISSUED the unload command, not the
	// resulting Changed bit — see fakeconn_test.go's own doc comment.
	if res.Failed {
		t.Fatalf("res = %+v, commands = %v", res, conn.Commands)
	}
	found := false
	for _, c := range conn.Commands {
		if c == "launchctl unload /Users/tester/Library/LaunchAgents/org.memcached.plist" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want an unload call", conn.Commands)
	}
}

func TestModuleLaunchdCtlFailure(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"launchctl list":      {RC: 0, Stdout: "1234\t0\torg.memcached\n"},
		`printf '%s' "$HOME"`: {RC: 0, Stdout: "/Users/tester"},
		"test -e /Users/tester/Library/LaunchAgents/org.memcached.plist":                   {RC: 0},
		"plutil -convert json -o - /Users/tester/Library/LaunchAgents/org.memcached.plist": {RC: 0, Stdout: launchdPlist(false, false)},
		"launchctl unload /Users/tester/Library/LaunchAgents/org.memcached.plist":          {RC: 1, Stderr: "boom"},
	})
	res, err := moduleLaunchd(context.Background(), conn, map[string]any{
		"name": "org.memcached", "state": "unloaded",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed when launchctl exits non-zero")
	}
}

func TestModuleLaunchdMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLaunchd(context.Background(), conn, map[string]any{"state": "started"}); err == nil {
		t.Fatal("want error for missing name")
	}
}
