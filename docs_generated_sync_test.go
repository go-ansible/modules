package modules

import (
	"bytes"
	"os"
	"testing"

	"github.com/go-ansible/modules/internal/gendocs"
)

// TestGeneratedDocsAreUpToDate regenerates the doc tables in memory and
// compares them byte for byte with what is committed.
//
// This is the check TestDocsCoversEveryRegisteredModule cannot make. A
// module whose doc comment is edited without running `go generate`
// keeps its key in the table, so every name still has a non-empty
// entry and the coverage test passes while ansible-doc prints the old
// text. Nothing caught that before this test existed.
func TestGeneratedDocsAreUpToDate(t *testing.T) {
	want, err := gendocs.Generate(".")
	if err != nil {
		t.Fatalf("regenerating the doc tables: %v", err)
	}

	// Everything on disk that the generator owns, so a shard left
	// behind after its last module was removed or renamed is reported
	// too, not just a missing or stale one.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() && gendocs.IsGenerated(e.Name()) {
			onDisk[e.Name()] = true
		}
	}
	if len(onDisk) == 0 {
		t.Fatal("found no generated doc files on disk, so this test would pass by comparing nothing")
	}

	for name, body := range want {
		got, err := os.ReadFile(name)
		if err != nil {
			t.Errorf("%s is missing — run `go generate ./...`: %v", name, err)
			continue
		}
		if !bytes.Equal(got, body) {
			t.Errorf("%s is stale — run `go generate ./...` (%d bytes committed, %d regenerated)", name, len(got), len(body))
		}
		delete(onDisk, name)
	}
	for name := range onDisk {
		t.Errorf("%s is a generated file the generator no longer produces — run `go generate ./...`", name)
	}
}

// TestGeneratedDocsStayUnderTheShardCeiling is the gate that replaced
// an override. The table used to be one file of 1.3 MiB, which the
// fleet push guard refuses to publish, so every pull request adding a
// module needed GITSAFE_ALLOW_BIGBLOB=1 (go-ansible/modules#142).
//
// It fails well before the push guard would, and it names the file
// that grew — which is the whole difference between this and finding
// out at push time that an afternoon's work cannot be published.
func TestGeneratedDocsStayUnderTheShardCeiling(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !gendocs.IsGenerated(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		checked++
		if info.Size() >= gendocs.MaxShardBytes {
			t.Errorf("%s is %d bytes, at or over the %d-byte ceiling: subdivide it (gendocs.shardKey) before the push guard refuses it",
				e.Name(), info.Size(), gendocs.MaxShardBytes)
		}
	}
	if checked == 0 {
		t.Fatal("found no generated doc files to measure, so this test would pass by measuring nothing")
	}
}

// TestDocsShardsDoNotOverlap asserts that no module name appears in two
// shards. docsMerge would silently keep the last one, and the result
// would be a correct-looking table with one module documented as
// another.
func TestDocsShardsDoNotOverlap(t *testing.T) {
	total := 0
	for _, shard := range docsShards {
		total += len(shard)
	}
	if total != len(Docs) {
		t.Errorf("the shards hold %d entries but Docs has %d, so %d name(s) appear in more than one shard",
			total, len(Docs), total-len(Docs))
	}
	if len(docsShards) < 2 {
		t.Errorf("Docs is built from %d shard(s); this test cannot detect an overlap with fewer than 2", len(docsShards))
	}
}
