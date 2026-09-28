package modules

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCopy implements Ansible's `copy` module: writes literal content
// or a local file to a path on the target, idempotently (skips the
// transfer when the destination already holds the same bytes) and
// optionally sets its mode.
//
// Args: dest (string, required); content (string) or src (local file
// path) — exactly one; mode (octal string).
func moduleCopy(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}

	wantBytes, cleanup, err := copySource(args)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	// Check mode reports what WOULD change without changing it. Every
	// decision below is already made by comparing current state to
	// wanted, so a dry run answers the same question and simply does not
	// act on the answer.
	check := InCheckMode(args)

	changed := false
	// diff stays zero unless --diff asked for one; Result.WithDiff
	// treats a zero Diff as nothing to report, so every exit below can
	// attach it unconditionally.
	var diff Diff
	// The local file the content is written through, which real reports
	// as src on a changed run.
	var staged string
	current, err := fetchIfExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	if current == nil || !bytes.Equal(current, wantBytes) {
		if InDiffMode(args) {
			// Real Ansible names the after side by where the bytes came
			// from: the dest path for an inline content:, the local
			// source path for a src:.
			afterHeader := dest
			if _, inline := args["content"]; !inline {
				if src := argString(args, "src", ""); src != "" {
					afterHeader = src
				}
			}
			diff = ContentDiff(dest, afterHeader, current, wantBytes)
		}
		if check {
			changed = true
			return copyResult(dest, changed).WithDiff(diff), nil
		}
		// Whether this is a NEW file decides its mode below, so it is
		// read before the write rather than after.
		isNew := current == nil
		tmp, err := os.CreateTemp("", "go-ansible-copy-*")
		if err != nil {
			return Result{}, fmt.Errorf("copy: %w", err)
		}
		tmpPath := tmp.Name()
		staged = tmpPath
		defer os.Remove(tmpPath)
		if _, err := tmp.Write(wantBytes); err != nil {
			tmp.Close()
			return Result{}, fmt.Errorf("copy: %w", err)
		}
		tmp.Close()

		if err := conn.Put(ctx, tmpPath, dest, remoteexec.PutOptions{MkdirParents: true}); err != nil {
			return Result{}, fmt.Errorf("copy: %w", err)
		}
		// A NEW file must end up with the mode the target's umask
		// implies, which is what real produces. The staging file
		// os.CreateTemp makes is always 0600 and Put carries that
		// across, so a copy: with no mode: created a 0600 file where
		// real creates 0644. An EXISTING file keeps the mode it had --
		// measured on both sides -- so this applies only to a new one,
		// and an explicit mode: below overrides it either way.
		if isNew && mode == nil {
			if err := applyUmaskDefault(ctx, conn, dest); err != nil {
				return Result{}, err
			}
		}
		changed = true
	}

	if mode != nil {
		info, err := statPath(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
		if info == nil || info.mode != *mode {
			if check {
				return copyResult(dest, true).WithDiff(diff), nil
			}
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	out := copyResult(dest, changed).WithDiff(diff)
	out = out.WithExtra("diff", copyDiffKey(dest, string(current), string(wantBytes), InDiffMode(args)))
	return withCopyFileKeys(ctx, conn, out, dest, staged, changed)
}

// copyResult is the module's single exit shape, so the check-mode
// early-returns above report exactly what the real path would.
// copyResult is the shape real reports, measured against ansible-core
// 2.21.4:
//
//	changed    changed checksum dest diff failed gid group md5sum mode
//	           owner size src state uid
//	unchanged  the same, with src and md5sum replaced by path
//
// There is NO msg key on either -- this port set one to the destination
// path -- and there are fourteen keys where this port reported three.
//
// src is real's own STAGING file (~/.ansible/tmp/…/.source.txt), not the
// source the caller named, so its value is a per-run temporary path on
// both sides and is never comparable between them. Only its presence is.
func copyResult(dest string, changed bool) Result {
	r := Ok("")
	if changed {
		r = Changed("")
	}
	r.NoMsg = true
	return r.WithExtra("dest", dest)
}

// withCopyFileKeys adds what real reports about the file that is now at
// dest. staged is the local file the content was written through, which
// real reports as src on a changed run.
func withCopyFileKeys(ctx context.Context, conn remoteexec.Connection, r Result, dest, staged string, changed bool) (Result, error) {
	dict, err := statDict(ctx, conn, dest, statOptions{GetChecksum: true})
	if err != nil {
		return r, err
	}
	for from, to := range map[string]string{
		"size": "size", "mode": "mode", "uid": "uid", "gid": "gid",
		"checksum": "checksum", "pw_name": "owner", "gr_name": "group",
	} {
		if v, ok := dict[from]; ok {
			r = r.WithExtra(to, v)
		}
	}
	r = r.WithExtra("state", "file")
	if changed {
		// A changed run names the staging file and its md5.
		r = r.WithExtra("src", staged)
		res, mderr := conn.Exec(ctx, "md5 -q "+shellQuote(dest)+" 2>/dev/null || md5sum "+shellQuote(dest)+" 2>/dev/null | awk '{print $1}'", nil)
		if mderr == nil {
			r = r.WithExtra("md5sum", strings.TrimSpace(res.Stdout))
		}
	} else {
		// An unchanged one names the path instead, and reports neither.
		r = r.WithExtra("path", dest)
	}
	return r, nil
}

// copySource resolves the copy module's content, from either `content`
// (a literal string) or `src` (a local file to read).
func copySource(args map[string]any) (data []byte, cleanup func(), err error) {
	noop := func() {}
	_, hasContent := args["content"]
	_, hasSrc := args["src"]
	// Real ansible-core refuses BOTH of these, with these exact
	// messages (measured on 2.21.4) — and refusing the second one
	// matters: giving src and content together silently used the
	// content here and reported ok, so a playbook that names a source
	// file copied something else entirely.
	if hasContent && hasSrc {
		return nil, noop, errArg("src and content are mutually exclusive")
	}
	if !hasContent && !hasSrc {
		return nil, noop, errArg("src (or content) is required")
	}
	if v, ok := args["content"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, noop, errArg("copy: content must be a string")
		}
		return []byte(s), noop, nil
	}
	src, err := requireString(args, "src")
	if err != nil {
		return nil, noop, errArg("src (or content) is required")
	}
	data, readErr := os.ReadFile(src)
	if readErr != nil {
		return nil, noop, fmt.Errorf("copy: reading src %q: %w", src, readErr)
	}
	return data, noop, nil
}

// fetchIfExists fetches path's content from conn, returning nil (not an
// error) if the path does not exist.
func fetchIfExists(ctx context.Context, conn remoteexec.Connection, path string) ([]byte, error) {
	exists, err := pathExists(ctx, conn, path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	tmp, err := os.CreateTemp("", "go-ansible-fetch-*")
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := conn.Fetch(ctx, path, tmpPath); err != nil {
		return nil, fmt.Errorf("fetching %s: %w", path, err)
	}
	return os.ReadFile(tmpPath)
}

// applyUmaskDefault sets path to the mode a freshly created file would
// have had: 0666 masked by the TARGET's umask, which is where the file
// lives. Real derives the same value from the umask of the process it
// runs the module with, so asking the target's shell is the faithful
// question rather than reading this process's own umask.
func applyUmaskDefault(ctx context.Context, conn remoteexec.Connection, path string) error {
	res, err := conn.Exec(ctx, "umask", nil)
	if err != nil {
		return fmt.Errorf("copy: reading the umask: %w", err)
	}
	mask, perr := strconv.ParseUint(strings.TrimSpace(res.Stdout), 8, 32)
	if perr != nil {
		// A shell that would not say: 022 is the near-universal default,
		// and a wrong guess here only mis-sets a mode the caller did not
		// specify.
		mask = 0o022
	}
	want := uint32(0o666) & ^uint32(mask)
	_, err = run(ctx, conn, fmt.Sprintf("chmod %04o %s", want, shellQuote(path)))
	return err
}
