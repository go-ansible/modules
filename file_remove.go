package modules

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFileRemove implements (a subset of) Ansible's `file_remove`
// module: removes files from ONE directory that match a glob or regex
// pattern — a pure local-filesystem operation on the target, no
// CLI/SaaS involvement at all, composed via `find` run through the
// Connection (matching find.go's own already-established
// GNU/BSD-portable `find` shape) plus a client-side (Go) filename match,
// the way find.go itself only ever filters by `-name` server-side and
// leaves everything else to its own caller.
//
// Args: path (string, required) — must already exist as a directory;
// pattern (string, required) — matched against each candidate's bare
// filename, never the full path, matching real file_remove's own
// documented behavior; use_regex (bool, default false) — interpret
// pattern as a regular expression instead of a glob; recursive (bool,
// default false) — search subdirectories too (never follows symlinked
// directories while doing so, matching real file_remove's own
// documented note — this port relies on `find`'s own default of not
// following symlinks unless `-L` is given, which this port never
// passes); file_type (file|link|any, default "file").
//
// Matching: a glob pattern is translated from Python fnmatch's own
// `[!seq]` negation spelling to Go's `path/filepath.Match` `[^seq]`
// spelling (the only syntax difference between the two for the
// wildcards real file_remove documents: `*`, `?`, `[seq]`, `[!seq]`)
// and matched against each candidate's bare filename via
// filepath.Match. A regex pattern is compiled with Go's RE2-based
// regexp package and matched via FindStringIndex (search-anywhere,
// unanchored) against the bare filename — real file_remove's own
// `regex.match(filename) or regex.search(filename)` reduces to exactly
// this (an unanchored search already covers everything an anchored
// match would find). RE2 does not support backreferences or lookaround
// assertions that a Python `re` pattern could use; a pattern relying on
// those is a documented gap, not a silent misinterpretation — it fails
// the argument-validation step below with a clear compile error instead
// of matching wrongly.
//
// Directories are never removed (nor even considered — `find`'s own
// `-type f`/`-type l` selection already excludes them, matching real
// file_remove's own should_include_file never matching a directory).
// This port has no check_mode/diff_mode plumbing (see module.go's own
// doc comment on this project's own architecture) so, unlike real
// file_remove, there is no dry-run preview — every matched file is
// removed immediately via one combined `rm -f` invocation.
func moduleFileRemove(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, errArg("file_remove: %v", err)
	}
	pattern, err := requireString(args, "pattern")
	if err != nil {
		return Result{}, errArg("file_remove: %v", err)
	}
	useRegex := argBool(args, "use_regex", false)
	recursive := argBool(args, "recursive", false)
	fileType := argString(args, "file_type", "file")

	info, err := statPath(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if info == nil {
		return Result{}, errArg("file_remove: path does not exist: %s", path)
	}
	if info.kind != fileKindDir {
		return Result{}, errArg("file_remove: path is not a directory: %s", path)
	}

	var matchFn func(basename string) bool
	if useRegex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return Result{}, errArg("file_remove: invalid regular expression pattern %q: %v", pattern, err)
		}
		matchFn = re.MatchString
	} else {
		goPattern := strings.ReplaceAll(pattern, "[!", "[^")
		matchFn = func(basename string) bool {
			ok, err := filepath.Match(goPattern, basename)
			return err == nil && ok
		}
	}

	cmd, err := fileRemoveFindCmd(path, recursive, fileType)
	if err != nil {
		return Result{}, err
	}
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	var candidates []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		candidates = append(candidates, line)
	}

	var matched []string
	for _, p := range candidates {
		if matchFn(filepath.Base(p)) {
			matched = append(matched, p)
		}
	}
	sort.Strings(matched)

	if len(matched) == 0 {
		return Ok(fmt.Sprintf("no files matched pattern %q", pattern)).
			WithExtra("removed_files", []string{}).
			WithExtra("files_count", 0).
			WithExtra("path", path), nil
	}

	quoted := make([]string, len(matched))
	for i, p := range matched {
		quoted[i] = shellQuote(p)
	}
	rmRes, err := runStatus(ctx, conn, "rm -f -- "+strings.Join(quoted, " "))
	if err != nil {
		return Result{}, err
	}
	if rmRes.RC != 0 {
		return Fail(fmt.Sprintf("file_remove: failed to remove some files: %s", strings.TrimSpace(rmRes.Stderr))).
			WithExtra("removed_files", []string{}).
			WithExtra("path", path), nil
	}

	return Changed(fmt.Sprintf("removed %d file(s) matching pattern %q", len(matched), pattern)).
		WithExtra("removed_files", matched).
		WithExtra("files_count", len(matched)).
		WithExtra("path", path), nil
}

// fileRemoveFindCmd builds the `find` invocation listing every
// candidate file/symlink under path, one full path per line — filename
// pattern matching itself happens client-side in Go (see
// moduleFileRemove's own doc comment on why).
func fileRemoveFindCmd(path string, recursive bool, fileType string) (string, error) {
	var b strings.Builder
	b.WriteString("find " + shellQuote(path) + " -mindepth 1")
	if !recursive {
		b.WriteString(" -maxdepth 1")
	}
	switch fileType {
	case "file":
		b.WriteString(" -type f")
	case "link":
		b.WriteString(" -type l")
	case "any":
		b.WriteString(" \\( -type f -o -type l \\)")
	default:
		return "", errArg("file_remove: file_type must be file, link, or any, got %q", fileType)
	}
	return b.String(), nil
}
