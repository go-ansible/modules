package modules

import (
	"context"
	"testing"
	"time"
)

func TestModuleAsyncStatusNotFound(t *testing.T) {
	conn := local()
	res, err := moduleAsyncStatus(context.Background(), conn, map[string]any{"jid": "no-such-job.12345"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed for a job id that was never launched")
	}
}

func TestModuleAsyncStatusRealJobLifecycle(t *testing.T) {
	conn := local()
	ctx := context.Background()

	jid, err := AsyncLaunch(ctx, conn, "sleep 0.3; echo done-output; exit 3")
	if err != nil {
		t.Fatal(err)
	}

	// Immediately after launch, the job should still be running (or,
	// indistinguishably, not yet have produced output — either way
	// "not finished").
	res, err := moduleAsyncStatus(ctx, conn, map[string]any{"jid": jid})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("status while running: Failed = true, res = %+v", res)
	}
	if res.Extra["finished"] != false {
		t.Fatalf("status while running: finished = %v, want false", res.Extra["finished"])
	}

	deadline := time.Now().Add(3 * time.Second)
	var final Result
	for time.Now().Before(deadline) {
		final, err = moduleAsyncStatus(ctx, conn, map[string]any{"jid": jid})
		if err != nil {
			t.Fatal(err)
		}
		if final.Extra["finished"] == true {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if final.Extra["finished"] != true {
		t.Fatal("job never finished within the deadline")
	}
	if !final.Failed {
		t.Fatalf("rc=3 should report Failed=true, got %+v", final)
	}
	if final.Extra["rc"] != 3 {
		t.Fatalf("rc = %v, want 3", final.Extra["rc"])
	}
	if final.Extra["stdout"] != "done-output\n" {
		t.Fatalf("stdout = %q", final.Extra["stdout"])
	}

	if _, err := moduleAsyncStatus(ctx, conn, map[string]any{"jid": jid, "mode": "cleanup"}); err != nil {
		t.Fatal(err)
	}
	after, err := moduleAsyncStatus(ctx, conn, map[string]any{"jid": jid})
	if err != nil {
		t.Fatal(err)
	}
	if !after.Failed {
		t.Fatal("want Failed (not found) after cleanup")
	}
}

func TestModuleAsyncStatusMissingJid(t *testing.T) {
	conn := local()
	if _, err := moduleAsyncStatus(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing jid")
	}
}

func TestModuleAsyncStatusInvalidMode(t *testing.T) {
	conn := local()
	if _, err := moduleAsyncStatus(context.Background(), conn, map[string]any{"jid": "1", "mode": "bogus"}); err == nil {
		t.Fatal("want error for invalid mode")
	}
}
