package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogrotateBuildConfigNginxSample(t *testing.T) {
	// Mirrors the exact directive spelling shown in real logrotate's own
	// ansible-doc RETURN VALUES sample for config_content (see
	// logrotate.go's doc comment).
	content, err := logrotateBuildConfig("nginx", map[string]any{
		"paths":            []string{"/var/log/nginx/*.log"},
		"rotation_period":  "daily",
		"rotate_count":     14,
		"compress":         true,
		"compress_options": "-9",
		"delay_compress":   true,
		"missing_ok":       true,
		"not_if_empty":     true,
		"create":           "0640 www-data adm",
		"shared_scripts":   true,
		"post_rotate":      []string{"[ -f /var/run/nginx.pid ] && kill -USR1 $(cat /var/run/nginx.pid)", "echo 'Nginx logs rotated'"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "/var/log/nginx/*.log {\n" +
		"    daily\n" +
		"    rotate 14\n" +
		"    compress\n" +
		"    compress_options -9\n" +
		"    delay_compress\n" +
		"    missing_ok\n" +
		"    notifempty\n" +
		"    create 0640 www-data adm\n" +
		"    shared_scripts\n" +
		"    post_rotate\n" +
		"        [ -f /var/run/nginx.pid ] && kill -USR1 $(cat /var/run/nginx.pid)\n" +
		"        echo 'Nginx logs rotated'\n" +
		"    endscript\n" +
		"}\n"
	if content != want {
		t.Fatalf("content =\n%q\nwant\n%q", content, want)
	}
}

func TestLogrotateBuildConfigMissingPaths(t *testing.T) {
	if _, err := logrotateBuildConfig("x", map[string]any{}); err == nil {
		t.Fatal("want error: paths required")
	}
}

func TestLogrotateBuildConfigSizeOverridesPeriodPresence(t *testing.T) {
	content, err := logrotateBuildConfig("myapp", map[string]any{
		"paths": []string{"/var/log/myapp/app.log"},
		"size":  "100M",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "    size 100M\n") {
		t.Fatalf("content = %q", content)
	}
}

func TestModuleLogrotateCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleLogrotate(context.Background(), conn, map[string]any{
		"name": "nginx", "config_dir": dir,
		"paths": []string{"/var/log/nginx/*.log"}, "rotation_period": "daily", "rotate_count": 14,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	data, err := os.ReadFile(filepath.Join(dir, "nginx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "/var/log/nginx/*.log {\n") {
		t.Fatalf("content = %q", data)
	}
	if res.Extra["config_file"] != filepath.Join(dir, "nginx") {
		t.Fatalf("config_file = %v", res.Extra["config_file"])
	}
}

func TestModuleLogrotateIdempotent(t *testing.T) {
	dir := t.TempDir()
	content, err := logrotateBuildConfig("nginx", map[string]any{
		"paths": []string{"/var/log/nginx/*.log"}, "rotation_period": "daily",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nginx"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleLogrotate(context.Background(), conn, map[string]any{
		"name": "nginx", "config_dir": dir,
		"paths": []string{"/var/log/nginx/*.log"}, "rotation_period": "daily",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleLogrotateBackupOnOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nginx"), []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleLogrotate(context.Background(), conn, map[string]any{
		"name": "nginx", "config_dir": dir,
		"paths": []string{"/var/log/nginx/*.log"}, "rotation_period": "weekly",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if res.Extra["backup_file"] == nil {
		t.Fatal("want backup_file in Extra (backup defaults to true)")
	}
	backupPath, _ := res.Extra["backup_file"].(string)
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("backup file not found: %v", err)
	}
	if string(data) != "old content\n" {
		t.Fatalf("backup content = %q", data)
	}
}

func TestModuleLogrotateDisabledRenamesAndCleansUpOther(t *testing.T) {
	dir := t.TempDir()
	// Simulate a previous enabled=true run leaving an active file behind.
	if err := os.WriteFile(filepath.Join(dir, "nginx"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleLogrotate(context.Background(), conn, map[string]any{
		"name": "nginx", "config_dir": dir,
		"paths": []string{"/var/log/nginx/*.log"}, "rotation_period": "daily", "enabled": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "nginx")); !os.IsNotExist(err) {
		t.Fatal("want the active (enabled) file removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "nginx.disabled")); err != nil {
		t.Fatal("want the .disabled file written")
	}
}

func TestModuleLogrotateAbsent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nginx"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	res, err := moduleLogrotate(context.Background(), conn, map[string]any{
		"name": "nginx", "config_dir": dir, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed")
	}
	if _, err := os.Stat(filepath.Join(dir, "nginx")); !os.IsNotExist(err) {
		t.Fatal("want file removed")
	}
}

func TestModuleLogrotateAbsentAlready(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleLogrotate(context.Background(), conn, map[string]any{
		"name": "nginx", "config_dir": dir, "state": "absent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged")
	}
}

func TestModuleLogrotateMissingName(t *testing.T) {
	conn := local()
	if _, err := moduleLogrotate(context.Background(), conn, map[string]any{}); err == nil {
		t.Fatal("want error for missing name")
	}
}

func TestModuleLogrotateInvalidState(t *testing.T) {
	conn := local()
	if _, err := moduleLogrotate(context.Background(), conn, map[string]any{
		"name": "x", "state": "bogus",
	}); err == nil {
		t.Fatal("want error for invalid state")
	}
}
