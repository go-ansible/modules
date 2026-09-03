package modules

import (
	"context"
	"path/filepath"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleArchive implements (a subset of) Ansible's `archive` module:
// creates or extends an archive from one or more paths already on the
// target, entirely via target-side shell commands (`tar`/`zip`/`gzip`/
// `bzip2`/`xz`) — the mirror image of unarchive.go.
//
// Args: path ([]string, required) — one or more absolute paths on the
// target to archive; dest (string) — required whenever more than one
// path is given, or a single path is archived (not just compressed —
// see below); when omitted for a single compressed-only path, it
// defaults to path + the format's extension, matching real archive's
// own documented default; format (bz2|gz|tar|xz|zip|zstd, default
// "gz"); force_archive (bool, default false) — force tar/zip wrapping
// even for a single file, matching real archive's own semantics;
// remove (bool, default false) — remove the source path(s) after a
// successful archive; force (bool, default false) — see Idempotency
// below.
//
// Single-file compression vs. archiving: matching real archive, when
// exactly one path is given, force_archive is false, and format is one
// of the compression-only formats (gz, bz2, xz, zstd), the path's bytes
// are compressed directly (`gzip -c path > dest`) rather than wrapped in
// a tar — real archive's own documented distinction between "compress"
// and "archive" dest_state. format=tar and format=zip always archive
// (there is no bare "tar" or "zip" compressor), and a directory path is
// always archived even under a compression-only format, since gzip/etc.
// cannot compress a directory.
//
// Multi-path archiving: this port does not replicate real archive's
// "arcroot" algorithm (computing the longest common ancestor of all
// given paths and preserving each path's structure relative to it).
// Instead, for tar-based formats it passes each path to `tar` as a
// separate `-C <dir> <base>` pair (GNU tar accepts multiple -C/member
// pairs in one invocation), storing each entry under its bare basename;
// for zip it re-invokes `zip -r <dest> <base>` once per path from within
// that path's own directory (`cd <dir> && zip -r <dest> <base>`),
// relying on zip's own behavior of appending to an existing archive
// file. Both approaches lose real archive's preserved relative nesting
// when paths share a deep common ancestor — documented rather than
// silently wrong.
//
// Idempotency: real archive inspects the existing dest archive's actual
// members (returned as dest_state/archived/missing/incomplete) to decide
// whether a rewrite is needed. This port has no shell-composable way to
// inspect archive members portably across tar/zip/gzip, so it falls back
// to get_url.go's own simplification: skip whenever dest already exists,
// unless force=true (an argument real archive does NOT have — added
// here purely for this idempotency shim, following get_url.go's
// precedent).
//
// Simplifications vs real archive: no exclude_path, exclusion_patterns,
// attributes/owner/group/mode/selinux (se*)/unsafe_writes; path
// elements are taken literally — no glob expansion (a caller wanting
// glob semantics must expand paths itself, e.g. via `find`); zstd format
// requires a `tar` build with --zstd support (GNU tar >= 1.31) rather
// than real archive's `zstandard` Python library — if unavailable, the
// tar invocation itself fails at runtime rather than being detected
// ahead of time.
func moduleArchive(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	paths := argStringList(args, "path")
	if len(paths) == 0 {
		return Result{}, errArg("archive: path is required and must not be empty")
	}
	dest := argString(args, "dest", "")
	format := argString(args, "format", "gz")
	if !archiveValidFormat(format) {
		return Result{}, errArg("archive: unknown format %q (choices: bz2, gz, tar, xz, zip, zstd)", format)
	}
	forceArchive := argBool(args, "force_archive", false)
	remove := argBool(args, "remove", false)
	force := argBool(args, "force", false)

	compressOnly := !forceArchive && len(paths) == 1 && archiveIsCompressFormat(format)
	if compressOnly {
		// A directory can't be gzip/bzip2/xz/zstd-compressed directly;
		// fall back to archiving it, matching real archive's own
		// dest_state distinction.
		info, err := statPath(ctx, conn, paths[0])
		if err != nil {
			return Result{}, err
		}
		if info != nil && info.kind == fileKindDir {
			compressOnly = false
		}
	}

	if dest == "" {
		if compressOnly {
			dest = paths[0] + archiveExtension(format)
		} else {
			return Result{}, errArg("archive: dest is required when archiving more than one path, a directory, or with force_archive")
		}
	}

	exists, err := pathExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	if exists && !force {
		return Ok(dest+" already exists").WithExtra("dest", dest), nil
	}

	var cmd string
	if compressOnly {
		cmd = archiveCompressCmd(paths[0], dest, format)
	} else if format == "zip" {
		cmd = archiveZipCmd(paths, dest)
	} else {
		cmd = archiveTarCmd(paths, dest, format)
	}
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}

	if remove {
		for _, p := range paths {
			if _, err := run(ctx, conn, "rm -rf "+shellQuote(p)); err != nil {
				return Result{}, err
			}
		}
	}

	return Changed(dest).WithExtra("dest", dest).WithExtra("format", format), nil
}

func archiveValidFormat(format string) bool {
	switch format {
	case "bz2", "gz", "tar", "xz", "zip", "zstd":
		return true
	}
	return false
}

func archiveIsCompressFormat(format string) bool {
	switch format {
	case "gz", "bz2", "xz", "zstd":
		return true
	}
	return false
}

func archiveExtension(format string) string {
	switch format {
	case "gz":
		return ".gz"
	case "bz2":
		return ".bz2"
	case "xz":
		return ".xz"
	case "zstd":
		return ".zst"
	case "tar":
		return ".tar"
	case "zip":
		return ".zip"
	}
	return ""
}

// archiveCompressCmd builds a direct (non-tar) compression invocation
// for a single file, for the formats that support it.
func archiveCompressCmd(path, dest, format string) string {
	q, d := shellQuote(path), shellQuote(dest)
	var tool string
	switch format {
	case "gz":
		tool = "gzip"
	case "bz2":
		tool = "bzip2"
	case "xz":
		tool = "xz"
	case "zstd":
		tool = "zstd"
	}
	return tool + " -c " + q + " > " + d
}

// archiveTarCmd builds a tar invocation covering all of paths, each
// contributed as its own `-C <dir> <base>` pair so paths need not share
// a common parent directory.
func archiveTarCmd(paths []string, dest, format string) string {
	var flags string
	switch format {
	case "tar":
		flags = "cf"
	case "gz":
		flags = "czf"
	case "bz2":
		flags = "cjf"
	case "xz":
		flags = "cJf"
	case "zstd":
		flags = "--zstd -cf"
	}
	var b strings.Builder
	b.WriteString("tar " + flags + " " + shellQuote(dest))
	for _, p := range paths {
		dir, base := filepath.Split(strings.TrimSuffix(p, "/"))
		if dir == "" {
			dir = "."
		}
		b.WriteString(" -C " + shellQuote(strings.TrimSuffix(dir, "/")) + " " + shellQuote(base))
	}
	return b.String()
}

// archiveZipCmd builds a chained `cd <dir> && zip -r <dest> <base>`
// invocation per path, relying on zip's own append-to-existing-archive
// behavior for the second and later paths.
func archiveZipCmd(paths []string, dest string) string {
	destQ := shellQuote(dest)
	parts := make([]string, 0, len(paths))
	for _, p := range paths {
		dir, base := filepath.Split(strings.TrimSuffix(p, "/"))
		if dir == "" {
			dir = "."
		}
		parts = append(parts, "(cd "+shellQuote(strings.TrimSuffix(dir, "/"))+" && zip -r "+destQ+" "+shellQuote(base)+")")
	}
	return strings.Join(parts, " && ")
}
