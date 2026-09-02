package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFind implements (a subset of) Ansible's `find` module: lists
// files under one or more paths matching simple criteria, by composing
// a POSIX `find` invocation on the target — unlike real
// ansible.builtin.find, which is a pure-Python walk that doesn't shell
// out to `find` at all; the observable result (a list of matching
// paths) is the same.
//
// Args: paths (string or []string, required); patterns (string or
// []string, glob, optional — matches everything when empty); recurse
// (bool, default false); file_type (file|directory|any, default
// "file").
//
// Each matched entry in Extra["files"] carries only "path" — real
// find's per-file dict also includes size/mode/mtime/checksum/etc via
// Python's os.stat, which this port does not replicate (a caller
// wanting that can `stat` each returned path itself).
//
// A path that doesn't exist, or a permission-denied subdirectory while
// recursing, makes POSIX find exit non-zero while still printing
// everything it did manage to find; unlike most other modules in this
// package, moduleFind does not treat that as a hard failure — it
// parses whatever stdout it got, matching find's own "best effort"
// behavior more closely than failing the whole task would.
func moduleFind(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	paths := argStringList(args, "paths")
	if len(paths) == 0 {
		return Result{}, errArg("find: missing required argument: paths")
	}
	patterns := argStringList(args, "patterns")
	recurse := argBool(args, "recurse", false)
	fileType := argString(args, "file_type", "file")

	cmd, err := findCmd(paths, patterns, recurse, fileType)
	if err != nil {
		return Result{}, err
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	var files []map[string]any
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, map[string]any{"path": line})
	}
	return Ok("").WithExtra("files", files).WithExtra("matched", len(files)), nil
}

// findCmd builds the `find` invocation for moduleFind, separated out so
// its exact shape can be asserted directly in tests.
func findCmd(paths, patterns []string, recurse bool, fileType string) (string, error) {
	var b strings.Builder
	b.WriteString("find")
	for _, p := range paths {
		b.WriteString(" " + shellQuote(p))
	}
	b.WriteString(" -mindepth 1")
	if !recurse {
		b.WriteString(" -maxdepth 1")
	}
	switch fileType {
	case "file":
		b.WriteString(" -type f")
	case "directory":
		b.WriteString(" -type d")
	case "any":
		// no -type filter: files, directories, and everything else.
	default:
		return "", errArg("find: file_type must be file, directory, or any, got %q", fileType)
	}
	if len(patterns) > 0 {
		b.WriteString(" \\(")
		for i, pat := range patterns {
			if i > 0 {
				b.WriteString(" -o")
			}
			b.WriteString(" -name " + shellQuote(pat))
		}
		b.WriteString(" \\)")
	}
	return b.String(), nil
}
