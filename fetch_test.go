package modules

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleFetchNew(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "remote.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "nested", "local.txt")
	conn := local()

	res, err := moduleFetch(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleFetchUnchangedWhenIdentical(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "remote.txt")
	dest := filepath.Join(dir, "local.txt")
	if err := os.WriteFile(src, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()

	res, err := moduleFetch(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("want unchanged when dest already matches src")
	}
}

func TestModuleFetchChangedWhenDifferent(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "remote.txt")
	dest := filepath.Join(dir, "local.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()

	res, err := moduleFetch(context.Background(), conn, map[string]any{"src": src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("want changed when dest differs from src")
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "new" {
		t.Fatalf("content = %q", data)
	}
}

func TestModuleFetchMissingSrcFails(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleFetch(context.Background(), conn, map[string]any{
		"src": filepath.Join(dir, "absent"), "dest": filepath.Join(dir, "out"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed by default when src is missing")
	}
}

func TestModuleFetchMissingSrcNoFail(t *testing.T) {
	dir := t.TempDir()
	conn := local()
	res, err := moduleFetch(context.Background(), conn, map[string]any{
		"src": filepath.Join(dir, "absent"), "dest": filepath.Join(dir, "out"), "fail_on_missing": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatal("want not Failed when fail_on_missing is false")
	}
}

func TestModuleFetchMissingArgs(t *testing.T) {
	conn := local()
	if _, err := moduleFetch(context.Background(), conn, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error for missing src")
	}
	if _, err := moduleFetch(context.Background(), conn, map[string]any{"src": "/x"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}

func TestModuleFetchSrcIsDirectoryFails(t *testing.T) {
	// src exists (pathExists is true for a directory too) but the
	// underlying Fetch/copyFile fails trying to read it as a file —
	// exercises the "conn.Fetch itself failed" branch distinct from
	// "src does not exist".
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "adir")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	conn := local()
	if _, err := moduleFetch(context.Background(), conn, map[string]any{
		"src": srcDir, "dest": filepath.Join(dir, "out"),
	}); err == nil {
		t.Fatal("want error when src is a directory")
	}
}

func TestModuleFetchMkdirFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "remote.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	conn := local()
	if _, err := moduleFetch(context.Background(), conn, map[string]any{
		"src": src, "dest": filepath.Join(blocker, "sub", "out"),
	}); err == nil {
		t.Fatal("want error when dest's parent path is blocked by a regular file")
	}
}

// Measured from real ansible-core 2.21.4:
//
//	changed    changed checksum dest failed file md5sum
//	           remote_checksum remote_md5sum
//	unchanged  the same without the two remote_ ones
//
// dest is the RESOLVED destination file, file is the remote source, and
// remote_md5sum is EMPTY -- real stopped filling it and kept the key.
func TestModuleFetchResultShape(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fe.txt")
	if err := os.WriteFile(src, []byte("fetched content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	into := filepath.Join(dir, "fetched") + string(os.PathSeparator)
	ctx := context.Background()
	conn := local()

	// A dest ending in a separator is a DIRECTORY: the file lands in it
	// under its own basename. Treating it as a file name failed with
	// "is a directory".
	res, err := moduleFetch(ctx, conn, map[string]any{"src": src, "dest": into, "flat": true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatal("first fetch was not changed")
	}
	wantDest := filepath.Join(dir, "fetched", "fe.txt")
	if res.Extra["dest"] != wantDest {
		t.Errorf("dest = %v, want %v", res.Extra["dest"], wantDest)
	}
	if res.Extra["file"] != src {
		t.Errorf("file = %v, want the remote source %v", res.Extra["file"], src)
	}
	if _, gone := res.Extra["src"]; gone {
		t.Error("src key present; real reports file, not src")
	}
	if !res.NoMsg {
		t.Error("a msg key is present; real has none")
	}
	// sha1 and md5 of "fetched content\n".
	if res.Extra["checksum"] != "cff2b1b552e314597436fa3cb4b5e2e033bf50a1" {
		t.Errorf("checksum = %v", res.Extra["checksum"])
	}
	if res.Extra["md5sum"] != "1637ed4080250eeeb622d1e835a3c5d9" {
		t.Errorf("md5sum = %v", res.Extra["md5sum"])
	}
	if res.Extra["remote_checksum"] != res.Extra["checksum"] {
		t.Errorf("remote_checksum = %v", res.Extra["remote_checksum"])
	}
	if res.Extra["remote_md5sum"] != "" {
		t.Errorf("remote_md5sum = %v, want empty", res.Extra["remote_md5sum"])
	}
	if got, err := os.ReadFile(wantDest); err != nil || string(got) != "fetched content\n" {
		t.Errorf("content = %q, err = %v", got, err)
	}

	// Fetching again changes nothing, and drops the two remote_ keys.
	res, err = moduleFetch(ctx, conn, map[string]any{"src": src, "dest": into, "flat": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("second fetch reported changed")
	}
	for _, k := range []string{"remote_checksum", "remote_md5sum"} {
		if _, ok := res.Extra[k]; ok {
			t.Errorf("unchanged fetch reported %q; real does not", k)
		}
	}
}
