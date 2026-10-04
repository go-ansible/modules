package modules

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestGetURLCmd(t *testing.T) {
	// Unconditional: no destination to compare against yet.
	cmd := getURLCmd("/tmp/st", "/opt/f", "https://example.com/f", false)
	want := "if command -v curl >/dev/null 2>&1; then curl -fsSL -o /tmp/st -w '%{http_code} %{time_total}' https://example.com/f" +
		"; elif command -v wget >/dev/null 2>&1; then wget -q -O /tmp/st https://example.com/f && printf '200 0'" +
		"; else echo 'get_url: neither curl nor wget found' >&2; exit 127; fi"
	if cmd != want {
		t.Fatalf("cmd = %q, want %q", cmd, want)
	}

	// Conditional: -z makes curl ask only if the destination is older,
	// which is what produces real's 304 on an unchanged file. Measured:
	// a second get_url against the same URL reports changed=false and
	// msg "HTTP Error 304: Not Modified" rather than downloading again.
	cond := getURLCmd("/tmp/st", "/opt/f", "https://example.com/f", true)
	if !strings.Contains(cond, "-z /opt/f") {
		t.Errorf("conditional form does not pass -z: %q", cond)
	}
	if strings.Contains(cmd, "-z ") {
		t.Errorf("unconditional form passes -z: %q", cmd)
	}
}

// These two used to drive a fake connection keyed by exact command
// strings, which stopped being workable once the module downloads
// through a STAGING file and then checksums, moves and md5s it -- four
// more commands, one of them with a path from conn.TempPath.
//
// They now use a real Local connection and a file:// source, so they
// are hermetic (no network) and exercise the actual commands. The key
// set asserted is real's, measured against ansible-core 2.21.4.
func TestModuleGetURLDownloadsAndReportsRealsKeys(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "got.txt")

	res, err := Default().Run(context.Background(), "get_url", remoteexec.NewLocal(),
		map[string]any{"url": "file://" + src, "dest": dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Error("downloading a file that was not there reported no change")
	}
	// The four download-only keys. Measured: a 200 run reports
	// eighteen keys and a 304 run fourteen, differing by exactly these.
	for _, k := range []string{"src", "checksum_src", "checksum_dest", "md5sum"} {
		if _, ok := res.Extra[k]; !ok {
			t.Errorf("missing download key %q that real reports", k)
		}
	}
	for _, k := range []string{"dest", "url", "status_code", "elapsed"} {
		if _, ok := res.Extra[k]; !ok {
			t.Errorf("missing key %q that real reports on every run", k)
		}
	}
	// checksum_dest is the EMPTY string when the destination did not
	// exist, which is real's own answer rather than an absent key.
	if res.Extra["checksum_dest"] != "" {
		t.Errorf("checksum_dest = %v, want empty for a destination that did not exist", res.Extra["checksum_dest"])
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("dest holds %q", got)
	}
}

// The second run reports no change. Its PREMISE changed with this
// commit: the old test was called SkipsWhenPresent and asserted the
// module did not download at all, which is not what real does -- real
// sends a CONDITIONAL request and reports changed=false on a 304. Over
// file:// there is no 304, so the unchanged verdict comes from
// checksum_src == checksum_dest instead, which is the other half of
// real's own rule.
func TestModuleGetURLSecondRunReportsUnchanged(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "got.txt")
	args := map[string]any{"url": "file://" + src, "dest": dest}

	if _, err := Default().Run(context.Background(), "get_url", remoteexec.NewLocal(), args); err != nil {
		t.Fatal(err)
	}
	res, err := Default().Run(context.Background(), "get_url", remoteexec.NewLocal(), args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Errorf("the second run reported changed: %+v", res.Extra)
	}
	if res.Extra["checksum_src"] != res.Extra["checksum_dest"] {
		t.Errorf("checksums differ on identical content: %v vs %v",
			res.Extra["checksum_src"], res.Extra["checksum_dest"])
	}
}

// force makes the request UNCONDITIONAL -- it forces the download, not
// the verdict. Measured against ansible-core 2.21.4 with identical
// content: changed=False, status_code=200 (it did download), and both
// checksums equal.
//
// The old version of this test asserted res.Changed, which pinned THIS
// PORT's behaviour rather than real's.
func TestModuleGetURLForceDownloadsButReportsUnchanged(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "got.txt")
	base := map[string]any{"url": "file://" + src, "dest": dest}
	if _, err := Default().Run(context.Background(), "get_url", remoteexec.NewLocal(), base); err != nil {
		t.Fatal(err)
	}
	res, err := Default().Run(context.Background(), "get_url", remoteexec.NewLocal(),
		map[string]any{"url": "file://" + src, "dest": dest, "force": true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Errorf("force reported changed on identical content; real reports false: %+v", res.Extra)
	}
	if res.Extra["checksum_src"] != res.Extra["checksum_dest"] {
		t.Errorf("checksums differ on identical content: %v vs %v",
			res.Extra["checksum_src"], res.Extra["checksum_dest"])
	}
}

func TestModuleGetURLDownloadFails(t *testing.T) {
	dir := t.TempDir()
	res, err := Default().Run(context.Background(), "get_url", remoteexec.NewLocal(),
		map[string]any{"url": "file://" + filepath.Join(dir, "nope.txt"), "dest": filepath.Join(dir, "d.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed when the source does not exist: %+v", res)
	}
}

func TestModuleGetURLMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "got.txt")
	res, err := Default().Run(context.Background(), "get_url", remoteexec.NewLocal(),
		map[string]any{"url": "file://" + src, "dest": dest, "mode": "0755"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	info, serr := os.Stat(dest)
	if serr != nil {
		t.Fatal(serr)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %o, want 755", info.Mode().Perm())
	}
}

func TestModuleGetURLMissingArgs(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGetURL(context.Background(), conn, map[string]any{"dest": "/x"}); err == nil {
		t.Fatal("want error for missing url")
	}
	if _, err := moduleGetURL(context.Background(), conn, map[string]any{"url": "https://x"}); err == nil {
		t.Fatal("want error for missing dest")
	}
}

func TestModuleGetURLBadMode(t *testing.T) {
	conn := newFakeConn(nil)
	if _, err := moduleGetURL(context.Background(), conn, map[string]any{
		"url": "https://x", "dest": "/x", "mode": "not-octal",
	}); err == nil {
		t.Fatal("want error for invalid mode")
	}
}
