// Command gendocs writes the docs_generated*.go sources for the
// modules package. All of the work is in the gendocs library; this
// command only puts its output on disk and removes what it no longer
// produces.
//
// Run via `go generate ./...` from the modules package (see the
// //go:generate directive in registry.go).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-ansible/modules/internal/gendocs"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gendocs:", err)
		os.Exit(1)
	}
}

func run() error {
	// go:generate runs with cwd set to the directory containing the
	// //go:generate directive — modules/ itself.
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	files, err := gendocs.Generate(dir)
	if err != nil {
		return err
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			return err
		}
	}

	// Remove shards that are no longer produced — a letter can lose
	// its last module. Left behind, such a file still declares a
	// docsShard var that the regenerated index no longer references,
	// which is a compile error rather than a silent problem, but the
	// error names an unused variable instead of what happened.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !gendocs.IsGenerated(e.Name()) {
			continue
		}
		if _, produced := files[e.Name()]; produced {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
