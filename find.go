package modules

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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
// Each matched entry in Extra["files"] carries the per-file stat
// dictionary real reports, which is a measured 32-key SUBSET of what
// the stat module returns: the pure-lstat fields, without the access
// checks, the checksum, the mime type or the Linux-only extras. See
// findFileKeys.
//
// Real also reports how many entries it walked (examined), which paths
// it could not walk (skipped_paths) and a warning per skipped path.
// Measured: examined counts every entry under the roots and EXCLUDES
// the roots themselves, directories included.
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

	// Which roots can be walked at all, and how many entries lie under
	// the ones that can. Real skips a path that is not a directory --
	// including a plain file named as a path -- and says so.
	skipped, examined, err := findSurvey(ctx, conn, paths, recurse)
	if err != nil {
		return Result{}, err
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	files := []map[string]any{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// One probe per matched file. Real walks in-process and pays
		// nothing extra for this; here it is a round trip each, which is
		// the price of reporting the fields a playbook actually reads.
		dict, serr := statDict(ctx, conn, line, statOptions{})
		if serr != nil {
			return Result{}, serr
		}
		entry := filterKeys(dict, findFileKeys)
		// path comes from the walk, not from the stat: real reports it
		// for every match, and an entry that lost it would be useless
		// to the caller even if the probe told us nothing else.
		entry["path"] = line
		files = append(files, entry)
	}

	out := Ok("").
		WithExtra("files", files).
		WithExtra("matched", len(files)).
		WithExtra("examined", examined).
		WithExtra("skipped_paths", skipped)
	if len(skipped) > 0 {
		warnings := make([]any, 0, len(skipped))
		for _, p := range sortedAnyKeys(skipped) {
			// Real's wording, trailing newline included.
			warnings = append(warnings, fmt.Sprintf(
				"Skipped '%s' path due to this access issue: %s\n", p, skipped[p]))
		}
		out = out.WithExtra("warnings", warnings)
	}
	return out, nil
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

// findFileKeys is the per-file dictionary real's find reports, measured
// key for key. It is the pure-lstat subset of the stat module's own
// dictionary: no exists, no readable/writeable/executable, no checksum
// or mime type, and none of the Linux-only extras.
var findFileKeys = []string{
	"atime", "blocks", "ctime", "dev", "disk_usage_bytes", "gid",
	"gr_name", "inode", "isblk", "ischr", "isdir", "isfifo", "isgid",
	"islnk", "isreg", "issock", "isuid", "mode", "mtime", "nlink",
	"path", "pw_name", "rgrp", "roth", "rusr", "size", "uid", "wgrp",
	"woth", "wusr", "xgrp", "xoth", "xusr",
}

func filterKeys(m map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

// findSurvey reports which of the given paths real would skip, and how
// many entries lie under the rest. Both come from one round trip: a
// path that is not a directory is skipped with real's own reason, and
// the survivors are counted with the same depth limit the search uses.
func findSurvey(ctx context.Context, conn remoteexec.Connection, paths []string, recurse bool) (map[string]any, int, error) {
	var b strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&b, "if [ -d %s ]; then printf 'DIR %%s\\n' %s; else printf 'SKIP %%s\\n' %s; fi\n",
			shellQuote(p), shellQuote(p), shellQuote(p))
	}
	res, err := conn.Exec(ctx, b.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("find: surveying paths: %w", err)
	}
	skipped := map[string]any{}
	var dirs []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		switch {
		case strings.HasPrefix(line, "DIR "):
			dirs = append(dirs, strings.TrimPrefix(line, "DIR "))
		case strings.HasPrefix(line, "SKIP "):
			p := strings.TrimPrefix(line, "SKIP ")
			skipped[p] = fmt.Sprintf("'%s' is not a directory", p)
		}
	}
	if len(dirs) == 0 {
		return skipped, 0, nil
	}

	// -mindepth 1 because real does not count the roots themselves.
	depth := "-mindepth 1 -maxdepth 1"
	if recurse {
		depth = "-mindepth 1"
	}
	quoted := make([]string, len(dirs))
	for i, d := range dirs {
		quoted[i] = shellQuote(d)
	}
	countCmd := fmt.Sprintf("find %s %s 2>/dev/null | wc -l", strings.Join(quoted, " "), depth)
	cres, err := conn.Exec(ctx, countCmd, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("find: counting examined entries: %w", err)
	}
	examined, _ := strconv.Atoi(strings.TrimSpace(cres.Stdout))
	return skipped, examined, nil
}

// sortedAnyKeys orders a map[string]any's keys, so the warnings come out
// in the same order on every run.
func sortedAnyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
