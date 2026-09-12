package modules

import (
	"bytes"
	"context"
	"fmt"
	"os"

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
	current, err := fetchIfExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	if current == nil || !bytes.Equal(current, wantBytes) {
		if check {
			changed = true
			return copyResult(dest, changed), nil
		}
		tmp, err := os.CreateTemp("", "go-ansible-copy-*")
		if err != nil {
			return Result{}, fmt.Errorf("copy: %w", err)
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)
		if _, err := tmp.Write(wantBytes); err != nil {
			tmp.Close()
			return Result{}, fmt.Errorf("copy: %w", err)
		}
		tmp.Close()

		if err := conn.Put(ctx, tmpPath, dest, remoteexec.PutOptions{MkdirParents: true}); err != nil {
			return Result{}, fmt.Errorf("copy: %w", err)
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
				return copyResult(dest, true), nil
			}
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	return copyResult(dest, changed), nil
}

// copyResult is the module's single exit shape, so the check-mode
// early-returns above report exactly what the real path would.
func copyResult(dest string, changed bool) Result {
	if changed {
		return Changed(dest)
	}
	return Ok(dest)
}

// copySource resolves the copy module's content, from either `content`
// (a literal string) or `src` (a local file to read).
func copySource(args map[string]any) (data []byte, cleanup func(), err error) {
	noop := func() {}
	if v, ok := args["content"]; ok {
		s, ok := v.(string)
		if !ok {
			return nil, noop, errArg("copy: content must be a string")
		}
		return []byte(s), noop, nil
	}
	src, err := requireString(args, "src")
	if err != nil {
		return nil, noop, errArg("copy: exactly one of content or src is required")
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
