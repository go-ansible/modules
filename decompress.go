package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDecompress implements (a subset of) Ansible's `decompress`
// module: decompresses a SINGLE compressed file on the target into a
// single output file (the inverse of a single archive.go compress-only
// invocation) — unlike unarchive.go, there is no multi-member tar/zip
// handling here at all.
//
// Args: src (string, required); dest (string, optional) — defaults to
// src with its format extension stripped, or src + "_decompressed" if
// src doesn't carry that extension, matching real decompress's own
// documented default derivation; format (gz|bz2|xz, default "gz");
// remove (bool, default false) — delete src after a successful
// decompression; mode (octal string, optional) — chmod dest afterward.
//
// Idempotency: this port decompresses to a target-side temp file first,
// then compares it against any existing dest with `cmp -s` before
// deciding whether to move it into place — so a re-run against an
// already-correctly-decompressed dest reports unchanged, without ever
// transferring bytes back to the control node. Real decompress's own
// idempotency is presumably equivalent in spirit (it is Python running
// on the target already); this port's shell-composed version is
// documented here since it's a real architectural difference, not just
// a simplification.
//
// Simplifications vs real decompress: no attributes/owner/group/
// selinux(se*)/unsafe_writes. zstd is not a supported format (real
// decompress itself only supports gz/bz2/xz — no zstd — matching that
// exactly, unlike archive.go's broader format set).
func moduleDecompress(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	src, err := requireString(args, "src")
	if err != nil {
		return Result{}, err
	}
	format := argString(args, "format", "gz")
	if !decompressValidFormat(format) {
		return Result{}, errArg("decompress: unknown format %q (choices: bz2, gz, xz)", format)
	}
	dest := argString(args, "dest", "")
	if dest == "" {
		dest = decompressDefaultDest(src, format)
	}
	remove := argBool(args, "remove", false)
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}

	tmp := conn.TempPath("decompress-" + decompressBase(dest))
	if _, err := run(ctx, conn, decompressCmd(src, tmp, format)); err != nil {
		return Result{}, err
	}

	changed := false
	destExists, err := pathExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	same := false
	if destExists {
		res, err := runStatus(ctx, conn, "cmp -s "+shellQuote(tmp)+" "+shellQuote(dest))
		if err != nil {
			return Result{}, err
		}
		same = res.RC == 0
	}
	if !same {
		if _, err := run(ctx, conn, "mv -f "+shellQuote(tmp)+" "+shellQuote(dest)); err != nil {
			return Result{}, err
		}
		changed = true
	} else {
		_ = conn.Remove(ctx, tmp) // best-effort cleanup, matching script.go's own pattern
	}

	if mode != nil {
		info, err := statPath(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
		if info == nil || info.mode != *mode {
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if remove {
		srcExists, err := pathExists(ctx, conn, src)
		if err != nil {
			return Result{}, err
		}
		if srcExists {
			if _, err := run(ctx, conn, "rm -f "+shellQuote(src)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	r := Ok(dest)
	if changed {
		r = Changed(dest)
	}
	return r.WithExtra("dest", dest), nil
}

func decompressValidFormat(format string) bool {
	switch format {
	case "gz", "bz2", "xz":
		return true
	}
	return false
}

func decompressExtension(format string) string {
	switch format {
	case "gz":
		return ".gz"
	case "bz2":
		return ".bz2"
	case "xz":
		return ".xz"
	}
	return ""
}

func decompressDefaultDest(src, format string) string {
	ext := decompressExtension(format)
	if strings.HasSuffix(src, ext) {
		return strings.TrimSuffix(src, ext)
	}
	return src + "_decompressed"
}

func decompressCmd(src, dest, format string) string {
	q, d := shellQuote(src), shellQuote(dest)
	var tool string
	switch format {
	case "gz":
		tool = "gunzip -c"
	case "bz2":
		tool = "bunzip2 -c"
	case "xz":
		tool = "xz -dc"
	}
	return tool + " " + q + " > " + d
}

func decompressBase(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
