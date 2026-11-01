package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCountSeparatesShippedFromTests is the distinction the tool exists
// for: the figure in the README is about the code that ships, and
// counting the tests into it is how the previous figure went wrong.
func TestCountSeparatesShippedFromTests(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "shellQuote(x) shellQuote(y)\n")
	write("b.go", "shellQuote(z)\n")
	write("a_test.go", "shellQuote(t)\n")
	// Neither of these may be counted or read.
	write("notes.md", "shellQuote( in prose\n")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join("sub", "c.go"), "shellQuote(deep)\n")

	c, err := count(dir, "shellQuote(")
	if err != nil {
		t.Fatal(err)
	}
	if c.shipHits != 3 || c.shipFiles != 2 {
		t.Errorf("shipped: %d hits in %d files, want 3 in 2", c.shipHits, c.shipFiles)
	}
	if c.testHits != 1 || c.testFiles != 1 {
		t.Errorf("tests: %d hits in %d files, want 1 in 1", c.testHits, c.testFiles)
	}
}

// A planted positive: the count must be able to come back zero for a
// literal that is genuinely absent, so that a zero elsewhere means
// something.
func TestCountReturnsZeroForAnAbsentLiteral(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := count(dir, "shellQuote(")
	if err != nil {
		t.Fatal(err)
	}
	if c.shipHits != 0 {
		t.Errorf("hits = %d, want 0", c.shipHits)
	}
	if c.shipFiles != 1 {
		t.Errorf("files = %d, want the one file that was read", c.shipFiles)
	}
}

// A directory with nothing to read is an error rather than a zero,
// because "0 call sites" and "I looked in the wrong place" are the two
// answers a count must never confuse.
func TestCountRefusesADirectoryWithNoShippedGoFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "only_test.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := count(dir, "shellQuote("); err == nil {
		t.Fatal("a directory with only tests in it was counted as an answer")
	}
	if _, err := count(filepath.Join(dir, "nope"), "shellQuote("); err == nil {
		t.Fatal("a missing directory was counted as an answer")
	}
}
