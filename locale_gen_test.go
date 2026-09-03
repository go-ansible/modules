package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestLocaleGenEntry(t *testing.T) {
	if got := localeGenEntry("de_CH.UTF-8"); got != "de_CH.UTF-8 UTF-8" {
		t.Fatalf("got %q", got)
	}
	if got := localeGenEntry("C"); got != "C" {
		t.Fatalf("got %q", got)
	}
}

func TestLocaleGenApplyEntryUncomments(t *testing.T) {
	out, changed := localeGenApplyEntry([]string{"# de_CH.UTF-8 UTF-8"}, "de_CH.UTF-8 UTF-8", "present")
	if !changed || out[0] != "de_CH.UTF-8 UTF-8" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestLocaleGenApplyEntryAppendsWhenNoTemplateLine(t *testing.T) {
	out, changed := localeGenApplyEntry(nil, "de_CH.UTF-8 UTF-8", "present")
	if !changed || len(out) != 1 || out[0] != "de_CH.UTF-8 UTF-8" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestLocaleGenApplyEntryAlreadyActive(t *testing.T) {
	out, changed := localeGenApplyEntry([]string{"de_CH.UTF-8 UTF-8"}, "de_CH.UTF-8 UTF-8", "present")
	if changed || len(out) != 1 {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestLocaleGenApplyEntryAbsentComments(t *testing.T) {
	out, changed := localeGenApplyEntry([]string{"de_CH.UTF-8 UTF-8"}, "de_CH.UTF-8 UTF-8", "absent")
	if !changed || out[0] != "# de_CH.UTF-8 UTF-8" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestModuleLocaleGenNoLocaleGenFileFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/locale.gen": {RC: 1},
	})
	res, err := moduleLocaleGen(context.Background(), conn, map[string]any{"name": []string{"de_CH.UTF-8"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want failed: no /etc/locale.gen (e.g. RHEL-family target)")
	}
}

func TestModuleLocaleGenPresentUncommentsAndRuns(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/locale.gen": {RC: 0},
		"cat /etc/locale.gen":     {RC: 0, Stdout: "# de_CH.UTF-8 UTF-8\n"},
	})
	res, err := moduleLocaleGen(context.Background(), conn, map[string]any{"name": []string{"de_CH.UTF-8"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["mechanism"] != "glibc" {
		t.Fatalf("mechanism = %v", res.Extra["mechanism"])
	}
	found := false
	for _, c := range conn.Commands {
		if c == "locale-gen" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want locale-gen run, commands = %v", conn.Commands)
	}
}

func TestModuleLocaleGenAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/locale.gen": {RC: 0},
		"cat /etc/locale.gen":     {RC: 0, Stdout: "en_GB.UTF-8 UTF-8\n"},
	})
	res, err := moduleLocaleGen(context.Background(), conn, map[string]any{
		"name": []string{"en_GB.UTF-8"}, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
}

func TestModuleLocaleGenMultipleNamesUnchanged(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"test -e /etc/locale.gen": {RC: 0},
		"cat /etc/locale.gen":     {RC: 0, Stdout: "en_GB.UTF-8 UTF-8\nnl_NL.UTF-8 UTF-8\n"},
	})
	res, err := moduleLocaleGen(context.Background(), conn, map[string]any{
		"name": []string{"en_GB.UTF-8", "nl_NL.UTF-8"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleLocaleGenMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleLocaleGen(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
