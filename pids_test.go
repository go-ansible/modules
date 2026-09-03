package modules

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// pidsSpawnSleeper starts a real, short-lived `sleep` process (killed
// and reaped via t.Cleanup) so modulePids has a known, real PID to find
// via `pgrep` on the local machine — this module's real backend needs
// no root and is portable to CI, so it's tested against a real
// remoteexec.Local connection rather than fakeConn (see
// fakeconn_test.go's own doc comment on when each pattern applies).
func pidsSpawnSleeper(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

func TestModulePidsByName(t *testing.T) {
	pid := pidsSpawnSleeper(t)
	conn := local()
	res, err := modulePids(context.Background(), conn, map[string]any{"name": "sleep"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	pids, ok := res.Extra["pids"].([]int)
	if !ok {
		t.Fatalf("pids Extra has wrong type: %T", res.Extra["pids"])
	}
	found := false
	for _, p := range pids {
		if p == pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("pids = %v, want to find spawned pid %d", pids, pid)
	}
}

func TestModulePidsByPattern(t *testing.T) {
	pid := pidsSpawnSleeper(t)
	conn := local()
	res, err := modulePids(context.Background(), conn, map[string]any{"pattern": "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	pids := res.Extra["pids"].([]int)
	found := false
	for _, p := range pids {
		if p == pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("pids = %v, want to find spawned pid %d", pids, pid)
	}
}

func TestModulePidsIgnoreCase(t *testing.T) {
	pid := pidsSpawnSleeper(t)
	conn := local()
	res, err := modulePids(context.Background(), conn, map[string]any{
		"pattern": "SLEEP 30", "ignore_case": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pids := res.Extra["pids"].([]int)
	found := false
	for _, p := range pids {
		if p == pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("pids = %v, want to find spawned pid %d (ignore_case)", pids, pid)
	}
}

func TestModulePidsNoMatch(t *testing.T) {
	conn := local()
	res, err := modulePids(context.Background(), conn, map[string]any{
		"name": "definitely-not-a-real-process-name-xyz123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	pids := res.Extra["pids"].([]int)
	if len(pids) != 0 {
		t.Fatalf("pids = %v, want empty", pids)
	}
}

func TestModulePidsInvalidPattern(t *testing.T) {
	conn := local()
	res, err := modulePids(context.Background(), conn, map[string]any{"pattern": "(unclosed"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for an invalid regular expression")
	}
}

func TestModulePidsRequiresExactlyOne(t *testing.T) {
	conn := local()
	if _, err := modulePids(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error when neither name nor pattern is given")
	}
	if _, err := modulePids(context.Background(), conn, map[string]any{
		"name": "sleep", "pattern": "sleep",
	}); err == nil {
		t.Fatal("want error when both name and pattern are given")
	}
}

func TestModulePidsTimingSanity(t *testing.T) {
	// Guard against a hung pgrep invocation silently making this test
	// slow rather than failing outright.
	start := time.Now()
	conn := local()
	if _, err := modulePids(context.Background(), conn, map[string]any{"name": "init"}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("modulePids took too long")
	}
}
