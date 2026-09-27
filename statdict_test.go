package modules

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// realStatKeys is the key set real ansible-core 2.21.4 reports for a
// regular file, measured. 48 keys; a symlink adds lnk_target and
// lnk_source.
var realStatKeys = []string{
	"atime", "attr_flags", "attributes", "birthtime", "block_size",
	"blocks", "charset", "checksum", "ctime", "dev", "device_type",
	"disk_usage_bytes", "executable", "exists", "flags", "generation",
	"gid", "gr_name", "inode", "isblk", "ischr", "isdir", "isfifo",
	"isgid", "islnk", "isreg", "issock", "isuid", "mimetype", "mode",
	"mtime", "nlink", "path", "pw_name", "readable", "rgrp", "roth",
	"rusr", "size", "uid", "version", "wgrp", "woth", "writeable",
	"wusr", "xgrp", "xoth", "xusr",
}

func TestStatDictOnARegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := statDict(context.Background(), local(), path, statOptions{GetChecksum: true, GetMime: true})
	if err != nil {
		t.Fatal(err)
	}

	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, realStatKeys) {
		t.Errorf("keys differ from real's\n got %v\nwant %v", keys, realStatKeys)
	}

	// The types real reports, which a playbook does arithmetic and
	// comparisons on.
	for _, tc := range []struct {
		key  string
		want string
	}{
		{"mode", "string"}, {"size", "int64"}, {"uid", "int64"},
		{"mtime", "float64"}, {"atime", "float64"}, {"ctime", "float64"},
		{"birthtime", "float64"}, {"isreg", "bool"}, {"exists", "bool"},
		{"pw_name", "string"}, {"checksum", "string"}, {"attributes", "[]interface {}"},
		{"attr_flags", "string"}, {"generation", "int"},
	} {
		if got[tc.key] == nil && tc.key != "version" {
			t.Errorf("%s is nil", tc.key)
			continue
		}
		if g := reflect.TypeOf(got[tc.key]).String(); g != tc.want {
			t.Errorf("%s is %s, want %s", tc.key, g, tc.want)
		}
	}
	if v, present := got["version"]; !present || v != nil {
		t.Errorf("version = %#v, want nil and present", v)
	}

	// Values that must be exactly right for this file.
	if got["mode"] != "0644" {
		t.Errorf("mode = %v, want 0644", got["mode"])
	}
	if got["size"] != int64(6) {
		t.Errorf("size = %v, want 6", got["size"])
	}
	if got["isreg"] != true || got["isdir"] != false || got["islnk"] != false {
		t.Errorf("type bits wrong: %v %v %v", got["isreg"], got["isdir"], got["islnk"])
	}
	// sha1 of "hello\n", which is what real reports.
	if got["checksum"] != "f572d396fae9206628714fb2ce00f72e94f2258f" {
		t.Errorf("checksum = %v", got["checksum"])
	}
	// Mode bits, not access checks: 0644 is readable by all, writable
	// only by the owner.
	for key, want := range map[string]bool{
		"rusr": true, "wusr": true, "xusr": false,
		"rgrp": true, "wgrp": false, "xgrp": false,
		"roth": true, "woth": false, "xoth": false,
		"isuid": false, "isgid": false,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if got["disk_usage_bytes"] != got["blocks"].(int64)*512 {
		t.Errorf("disk_usage_bytes = %v, blocks = %v", got["disk_usage_bytes"], got["blocks"])
	}
}

func TestStatDictOnADirectoryAndASymlink(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "d")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := statDict(context.Background(), local(), sub, statOptions{GetMime: true})
	if err != nil {
		t.Fatal(err)
	}
	if got["isdir"] != true || got["isreg"] != false {
		t.Errorf("directory reported as isdir=%v isreg=%v", got["isdir"], got["isreg"])
	}
	// Real reports no checksum for a directory.
	if c, present := got["checksum"]; present && c != "" {
		t.Errorf("checksum = %v for a directory", c)
	}

	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// Real does NOT follow by default, so the link is reported as a link
	// and carries the two lnk_ keys.
	got, err = statDict(context.Background(), local(), link, statOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got["islnk"] != true {
		t.Errorf("islnk = %v; real does not follow by default", got["islnk"])
	}
	if got["lnk_target"] != target {
		t.Errorf("lnk_target = %v, want %v", got["lnk_target"], target)
	}
	if _, present := got["lnk_source"]; !present {
		t.Error("lnk_source missing")
	}

	// With follow, it is the target that is described and the lnk_ keys
	// are gone.
	got, err = statDict(context.Background(), local(), link, statOptions{Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	if got["islnk"] != false || got["isreg"] != true {
		t.Errorf("follow: islnk=%v isreg=%v", got["islnk"], got["isreg"])
	}
}

func TestStatDictOnAMissingPath(t *testing.T) {
	// Measured: exactly two keys.
	got, err := statDict(context.Background(), local(),
		filepath.Join(t.TempDir(), "nope"), statOptions{GetChecksum: true, GetMime: true})
	if err != nil {
		t.Fatal(err)
	}
	if got["exists"] != false {
		t.Errorf("exists = %v", got["exists"])
	}
	// Measured: exactly one key, exists. Not even path.
	if len(got) != 1 {
		t.Errorf("a missing path reported %d keys, want 1: %v", len(got), got)
	}
}

func TestAssembleStatMapsTheProbeWithoutAConnection(t *testing.T) {
	// The probe's own output shape, so the mapping is checked against
	// measured values rather than against whatever this machine has.
	raw := map[string]string{
		"exists":     "true",
		"fields":     "6|644|Regular File|501|0|alice|wheel|265220898|16777231|1|8|4096|0|0",
		"times":      "1790513957.705601|1790513957.705681|1790513957.705681|1790513957.705601",
		"readable":   "true",
		"writeable":  "true",
		"executable": "false",
		"checksum":   "f572d396fae9206628714fb2ce00f72e94f2258f",
		"mimetype":   "text/plain",
		"charset":    "us-ascii",
	}
	got := assembleStat("/tmp/f.txt", raw)
	for key, want := range map[string]any{
		"size": int64(6), "mode": "0644", "uid": int64(501), "gid": int64(0),
		"pw_name": "alice", "gr_name": "wheel", "inode": int64(265220898),
		"nlink": int64(1), "blocks": int64(8), "block_size": int64(4096),
		"disk_usage_bytes": int64(4096), "isreg": true, "isdir": false,
		"readable": true, "executable": false, "mimetype": "text/plain",
		"charset": "us-ascii", "path": "/tmp/f.txt",
	} {
		if got[key] != want {
			t.Errorf("%s = %#v, want %#v", key, got[key], want)
		}
	}
	if got["mtime"] != 1790513957.705681 {
		t.Errorf("mtime = %#v", got["mtime"])
	}
	// A BSD type word and a GNU one must land on the same answer.
	for word, wantDir := range map[string]bool{"Regular File": false, "regular file": false, "Directory": true, "directory": true} {
		raw["fields"] = "6|644|" + word + "|501|0|alice|wheel|1|1|1|8|4096|0|0"
		if assembleStat("/x", raw)["isdir"] != wantDir {
			t.Errorf("%q: isdir = %v, want %v", word, assembleStat("/x", raw)["isdir"], wantDir)
		}
	}
}

func TestFindFileKeysIsRealsSubset(t *testing.T) {
	// Real's find reports 33 keys per file (32 measured plus path),
	// every one of which the stat dictionary also has.
	statKeys := map[string]bool{}
	for _, k := range realStatKeys {
		statKeys[k] = true
	}
	for _, k := range findFileKeys {
		if !statKeys[k] {
			t.Errorf("find reports %q, which stat does not", k)
		}
	}
	for _, k := range []string{"exists", "checksum", "mimetype", "readable", "version"} {
		if strings.Contains(strings.Join(findFileKeys, ","), k) {
			t.Errorf("find reports %q; real's find does not", k)
		}
	}
}
