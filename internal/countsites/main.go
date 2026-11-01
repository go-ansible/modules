// Command countsites counts occurrences of a literal across the .go
// files of a directory, separating the files that ship from the tests.
//
// It exists because the README quotes a `shellQuote` call-site count as
// a security claim, and a claim like that has to be reproducible by the
// next reader. The figure it replaced -- "1 718 call sites across 620
// non-test files" -- paired the total INCLUDING tests with the count of
// files EXCLUDING them, two populations read as one, which is the shape
// of mistake a number maintained by hand eventually makes.
//
//	go run ./internal/countsites -dir . -literal 'shellQuote('
//
// It reports what it could not read rather than counting it as zero: a
// directory with no non-test .go files is an error, not an empty answer.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	var (
		dir     = flag.String("dir", ".", "directory of .go files to read")
		literal = flag.String("literal", "", "the text to count (required)")
	)
	flag.Parse()
	if *literal == "" {
		fmt.Fprintln(os.Stderr, "countsites: -literal is required")
		os.Exit(2)
	}
	c, err := count(*dir, *literal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "countsites:", err)
		os.Exit(1)
	}
	fmt.Printf("%q: %d in %d non-test files, %d in %d test files, %d total\n",
		*literal, c.shipHits, c.shipFiles, c.testHits, c.testFiles, c.shipHits+c.testHits)
}

type counts struct {
	shipFiles, testFiles int
	shipHits, testHits   int
}

func count(dir, literal string) (counts, error) {
	var c counts
	entries, err := os.ReadDir(dir)
	if err != nil {
		return c, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return c, err
		}
		hits := strings.Count(string(data), literal)
		if strings.HasSuffix(name, "_test.go") {
			c.testFiles++
			c.testHits += hits
			continue
		}
		c.shipFiles++
		c.shipHits += hits
	}
	if c.shipFiles == 0 {
		return c, fmt.Errorf("no non-test .go files in %s: wrong directory, not an empty package", dir)
	}
	return c, nil
}
