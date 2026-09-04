package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleSorceryPresentCasts(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gaze -q version foo": {RC: 0, Stdout: "hdr1\nhdr2\nx|y|foo|1.0|-\n\n"},
		"cast -c foo":         {RC: 0},
	})
	res, err := moduleSorcery(context.Background(), conn, map[string]any{"name": "foo", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want changed, res = %+v", res)
	}
}

func TestModuleSorceryPresentAlreadyCast(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gaze -q version foo": {RC: 0, Stdout: "hdr1\nhdr2\nx|y|foo|1.0|1.0\n\n"},
	})
	res, err := moduleSorcery(context.Background(), conn, map[string]any{"name": "foo", "state": "present"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
	for _, c := range conn.Commands {
		if c == "cast -c foo" {
			t.Fatal("should not have cast an already-installed spell")
		}
	}
}

func TestModuleSorceryLatestUpgrades(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gaze -q version foo": {RC: 0, Stdout: "hdr1\nhdr2\nx|y|foo|2.0|1.0\n\n"},
		"cast -c foo":         {RC: 0},
	})
	res, err := moduleSorcery(context.Background(), conn, map[string]any{"name": "foo", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSorceryAbsentDispels(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"gaze -q version foo": {RC: 0, Stdout: "hdr1\nhdr2\nx|y|foo|1.0|1.0\n\n"},
		"dispel foo":          {RC: 0},
	})
	res, err := moduleSorcery(context.Background(), conn, map[string]any{"name": "foo", "state": "dispelled"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	found := false
	for _, c := range conn.Commands {
		if c == "dispel foo" {
			found = true
		}
	}
	if !found {
		t.Fatal("want dispel foo to have run")
	}
}

func TestModuleSorceryWildcardLatestNoopWhenQueueEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sorcery queue": {RC: 0},
		"stat -c %s /var/log/sorcery/queue/install 2>/dev/null || stat -f %z /var/log/sorcery/queue/install 2>/dev/null || echo 0": {RC: 0, Stdout: "0\n"},
	})
	res, err := moduleSorcery(context.Background(), conn, map[string]any{"name": "*", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when queue is empty")
	}
}

func TestModuleSorceryWildcardLatestUpdatesWhenQueueNonEmpty(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sorcery queue": {RC: 0},
		"stat -c %s /var/log/sorcery/queue/install 2>/dev/null || stat -f %z /var/log/sorcery/queue/install 2>/dev/null || echo 0": {RC: 0, Stdout: "42\n"},
		"cast --queue": {RC: 0},
	})
	res, err := moduleSorcery(context.Background(), conn, map[string]any{"name": "*", "state": "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSorceryCommaSplitNames(t *testing.T) {
	names := sorceryNameList(map[string]any{"name": "foo,bar,baz"})
	if len(names) != 3 || names[0] != "foo" || names[1] != "bar" || names[2] != "baz" {
		t.Fatalf("names = %v", names)
	}
}

func TestModuleSorceryMissingRequired(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSorcery(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when none of name/update/update_cache given")
	}
}

func TestModuleSorceryInvalidState(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleSorcery(context.Background(), conn, map[string]any{"name": "foo", "state": "bogus"}); err == nil {
		t.Fatal("want error")
	}
}

func TestModuleSorceryUpdate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"sorcery update": {RC: 0},
	})
	res, err := moduleSorcery(context.Background(), conn, map[string]any{"update": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleSorceryGrimoireOfficial(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"scribe index":      {RC: 0, Stdout: "[1] : stable : /path : 1.0\n"},
		"scribe add binary": {RC: 0},
	})
	res, err := moduleSorcery(context.Background(), conn, map[string]any{
		"name": "binary", "repository": "*", "state": "present",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}
