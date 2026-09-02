package modules

import (
	"context"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestApplyCronEntryAppendsNew(t *testing.T) {
	out, changed := applyCronEntry(nil, "# ansible: job1", "present", map[string]any{"job": "echo hi"})
	if !changed {
		t.Fatal("want changed")
	}
	if len(out) != 2 || out[0] != "# ansible: job1" {
		t.Fatalf("out = %v", out)
	}
}

func TestApplyCronEntryReplacesExisting(t *testing.T) {
	existing := []string{"# ansible: job1", "* * * * * old", "# ansible: job2", "* * * * * other"}
	out, changed := applyCronEntry(existing, "# ansible: job1", "present", map[string]any{"job": "new"})
	if !changed {
		t.Fatal("want changed")
	}
	// The updated entry moves to the end (remove old + append new); job2
	// stays intact wherever it lands.
	if len(out) != 4 {
		t.Fatalf("out = %v, want job2 untouched + replaced job1", out)
	}
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "# ansible: job2\n* * * * * other") {
		t.Fatalf("job2 entry disturbed: out = %v", out)
	}
	if !strings.Contains(joined, "# ansible: job1\n* * * * * new") {
		t.Fatalf("job1 entry not updated: out = %v", out)
	}
}

func TestApplyCronEntryAbsent(t *testing.T) {
	existing := []string{"# ansible: job1", "* * * * * old"}
	out, changed := applyCronEntry(existing, "# ansible: job1", "absent", nil)
	if !changed {
		t.Fatal("want changed")
	}
	if len(out) != 0 {
		t.Fatalf("out = %v", out)
	}
}

func TestApplyCronEntryAbsentNotFound(t *testing.T) {
	existing := []string{"# ansible: other", "* * * * * x"}
	out, changed := applyCronEntry(existing, "# ansible: job1", "absent", nil)
	if changed {
		t.Fatal("want unchanged: marker not present")
	}
	if len(out) != 2 {
		t.Fatalf("out = %v", out)
	}
}

func TestCronScheduleLine(t *testing.T) {
	got := cronScheduleLine(map[string]any{"minute": "0", "hour": "3", "job": "backup.sh"})
	want := "0 3 * * * backup.sh"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestModuleCronPresentViaFakeConn(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"crontab -l 2>/dev/null": {RC: 1}, // no crontab yet
	})
	res, err := moduleCron(context.Background(), conn, map[string]any{
		"name": "job1", "job": "echo hi", "minute": "*/5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if len(conn.Commands) != 2 || conn.Commands[1] != "crontab -" {
		t.Fatalf("commands = %v", conn.Commands)
	}
	wantStdin := "# ansible: job1\n*/5 * * * * echo hi\n"
	if conn.Stdins[1] != wantStdin {
		t.Fatalf("stdin = %q, want %q", conn.Stdins[1], wantStdin)
	}
}

func TestModuleCronMissingName(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleCron(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}
