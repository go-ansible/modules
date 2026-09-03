package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPamLimitsCompareValue(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"10", "20", -1},
		{"20", "10", 1},
		{"10", "10", 0},
		{"unlimited", "10", 1},
		{"10", "infinity", -1},
		{"-1", "unlimited", 0},
	}
	for _, c := range cases {
		if got := pamLimitsCompareValue(c.a, c.b); got != c.want {
			t.Errorf("compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestPamLimitsApplyEntryAppendsNew(t *testing.T) {
	out, changed := pamLimitsApplyEntry(nil, "joe", "soft", "nofile", "64000", "", false, false)
	if !changed || len(out) != 1 || out[0] != "joe soft nofile 64000" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestPamLimitsApplyEntryReplacesExisting(t *testing.T) {
	existing := []string{"joe soft nofile 1000"}
	out, changed := pamLimitsApplyEntry(existing, "joe", "soft", "nofile", "64000", "", false, false)
	if !changed || out[0] != "joe soft nofile 64000" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestPamLimitsApplyEntryUseMaxKeepsLarger(t *testing.T) {
	existing := []string{"smith hard fsize 2000000"}
	out, changed := pamLimitsApplyEntry(existing, "smith", "hard", "fsize", "1000000", "", true, false)
	if changed {
		t.Fatal("want unchanged: existing value already larger")
	}
	if out[0] != "smith hard fsize 2000000" {
		t.Fatalf("out=%v", out)
	}
}

func TestPamLimitsApplyEntryUseMaxTakesNewLarger(t *testing.T) {
	existing := []string{"smith hard fsize 500"}
	out, changed := pamLimitsApplyEntry(existing, "smith", "hard", "fsize", "1000000", "", true, false)
	if !changed || out[0] != "smith hard fsize 1000000" {
		t.Fatalf("out=%v changed=%v", out, changed)
	}
}

func TestPamLimitsApplyEntryUseMinKeepsSmaller(t *testing.T) {
	existing := []string{"joe soft nofile 100"}
	out, changed := pamLimitsApplyEntry(existing, "joe", "soft", "nofile", "500", "", false, true)
	if changed {
		t.Fatal("want unchanged: existing value already smaller")
	}
	if out[0] != "joe soft nofile 100" {
		t.Fatalf("out=%v", out)
	}
}

func TestPamLimitsApplyEntryComment(t *testing.T) {
	out, changed := pamLimitsApplyEntry(nil, "james", "-", "memlock", "unlimited", "unlimited memory lock for james", false, false)
	if !changed || out[0] != "james - memlock unlimited\t# unlimited memory lock for james" {
		t.Fatalf("out=%v", out)
	}
}

func TestModulePamLimitsCreatesFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "limits.conf")
	conn := local()
	res, err := modulePamLimits(context.Background(), conn, map[string]any{
		"domain": "joe", "limit_type": "soft", "limit_item": "nofile", "value": "64000", "dest": f,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "joe soft nofile 64000\n" {
		t.Fatalf("content = %q", data)
	}
}

func TestModulePamLimitsIdempotent(t *testing.T) {
	f := filepath.Join(t.TempDir(), "limits.conf")
	if err := os.WriteFile(f, []byte("joe soft nofile 64000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := modulePamLimits(context.Background(), conn, map[string]any{
		"domain": "joe", "limit_type": "soft", "limit_item": "nofile", "value": "64000", "dest": f,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModulePamLimitsBackup(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "limits.conf")
	if err := os.WriteFile(f, []byte("joe soft nofile 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := modulePamLimits(context.Background(), conn, map[string]any{
		"domain": "joe", "limit_type": "soft", "limit_item": "nofile", "value": "2", "dest": f, "backup": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	foundBackup := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "limits.conf.") {
			foundBackup = true
		}
	}
	if !foundBackup {
		t.Fatalf("no backup file found in %v", entries)
	}
}

func TestModulePamLimitsMissingArgs(t *testing.T) {
	conn := local()
	if _, err := modulePamLimits(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing domain")
	}
	if _, err := modulePamLimits(context.Background(), conn, map[string]any{"domain": "joe"}); err == nil {
		t.Fatal("want error for missing limit_type")
	}
	if _, err := modulePamLimits(context.Background(), conn, map[string]any{
		"domain": "joe", "limit_type": "soft",
	}); err == nil {
		t.Fatal("want error for missing limit_item")
	}
	if _, err := modulePamLimits(context.Background(), conn, map[string]any{
		"domain": "joe", "limit_type": "soft", "limit_item": "nofile",
	}); err == nil {
		t.Fatal("want error for missing value")
	}
}
