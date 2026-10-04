package modules

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUnarchive implements (a subset of) Ansible's `unarchive`
// module: extracts an archive on the target, picking `tar` or `unzip`
// by the archive's file extension.
//
// Args: src (string, required) — a local path (uploaded first via
// conn.Put) or, when remote_src=true, a path already on the target;
// dest (string, required) — must already exist, matching real
// unarchive's own documented requirement (this port does not create
// it); remote_src (bool, default false); creates (string, optional,
// target path — same short-circuit as command/shell/script).
//
// Supported archive types, by extension: .tar, .tar.gz/.tgz,
// .tar.bz2/.tbz2, .tar.xz/.txz (all via `tar`), and .zip (via
// `unzip`) — an unrecognized extension fails cleanly rather than
// guessing from the file's contents the way real unarchive (via
// Python's tarfile/zipfile sniffing) can.
//
// Simplifications vs real unarchive: no `include`/`exclude` member
// filtering, `list_files`, `extra_opts`, `owner`/`group`/`mode`
// post-extraction, `validate_certs`, or `decrypt`. Idempotency is NOT
// checked — real unarchive compares each archive member's checksum
// against what's already at dest and only extracts what changed; this
// port always extracts and reports changed, since replicating a
// per-member checksum comparison purely through shell composition was
// judged out of scope for this batch — a real gap versus real
// unarchive's idempotent behavior, documented rather than silently
// claimed (the same tradeoff assemble.go makes, for the same reason).
func moduleUnarchive(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	src, err := requireString(args, "src")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	remoteSrc := argBool(args, "remote_src", false)

	if skip, msg, err := skipByCreatesRemoves(ctx, conn, args); err != nil {
		return Result{}, err
	} else if skip {
		return Ok(msg), nil
	}

	archivePath := src
	if !remoteSrc {
		archivePath = conn.TempPath(filepath.Base(src))
		if err := conn.Put(ctx, src, archivePath, remoteexec.PutOptions{}); err != nil {
			return Result{}, err
		}
	}

	cmd, handler, err := unarchiveCmd(archivePath, dest)
	if err != nil {
		return Result{}, err
	}
	// runStatus rather than run, because extract_results reports the
	// command's rc/out/err and run() discards them. The non-zero check
	// below keeps run()'s own behaviour and wording, so a failing
	// extraction fails exactly as it did before.
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, fmt.Errorf("running %q: %w", cmd, err)
	}
	if res.RC != 0 {
		return Result{}, fmt.Errorf("running %q: exit %d: %s", cmd, res.RC, strings.TrimSpace(res.Stderr))
	}

	if !remoteSrc {
		_ = conn.Remove(ctx, archivePath) // best-effort cleanup, see script.go's same pattern
	}

	// Real's key set on a successful extraction, measured on a .zip
	// that both engines handle:
	//
	//	changed,dest,extract_results,failed,gid,group,handler,mode,owner,size,src,state,uid
	//
	// and no msg. This returned Changed(dest), so three keys came back
	// and `msg` held the DESTINATION PATH. `dest` in Extra is also what
	// makes addPathInfo fill in the file attributes -- it reports
	// state=directory here, which is what real reports too.
	out := Changed("")
	out.NoMsg = true
	return out.WithExtra("src", src).
		WithExtra("dest", dest).
		WithExtra("handler", handler).
		WithExtra("extract_results", map[string]any{
			"cmd": cmd, "rc": res.RC, "out": res.Stdout, "err": res.Stderr,
		}), nil
}

// unarchiveCmd builds the tar/unzip invocation for moduleUnarchive,
// separated out so its exact shape (and extension dispatch) can be
// asserted directly in tests.
// unarchiveCmd returns the extraction command and the HANDLER NAME real
// reports for that archive type. Real's names are its own Python handler
// classes -- it tries [ZipArchive, ZipZArchive, TgzArchive, TarArchive,
// TarBzipArchive, TarXzArchive, TarZstdArchive] in order and reports
// whichever answered can_handle_archive() -- and a playbook reading
// r.handler reads one of those strings, so they are used verbatim rather
// than invented.
func unarchiveCmd(archivePath, dest string) (cmd, handler string, err error) {
	lower := strings.ToLower(archivePath)
	q, d := shellQuote(archivePath), shellQuote(dest)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return "tar xzf " + q + " -C " + d, "TgzArchive", nil
	case strings.HasSuffix(lower, ".tar.bz2"), strings.HasSuffix(lower, ".tbz2"):
		return "tar xjf " + q + " -C " + d, "TarBzipArchive", nil
	case strings.HasSuffix(lower, ".tar.xz"), strings.HasSuffix(lower, ".txz"):
		return "tar xJf " + q + " -C " + d, "TarXzArchive", nil
	case strings.HasSuffix(lower, ".tar"):
		return "tar xf " + q + " -C " + d, "TarArchive", nil
	case strings.HasSuffix(lower, ".zip"):
		return "unzip -o " + q + " -d " + d, "ZipArchive", nil
	default:
		return "", "", errArg("unarchive: unrecognized archive extension for %q (supported: .tar, .tar.gz/.tgz, .tar.bz2/.tbz2, .tar.xz/.txz, .zip)", archivePath)
	}
}
