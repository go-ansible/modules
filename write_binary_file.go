package modules

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleWriteBinaryFile implements Ansible's `write_binary_file`
// (community.general) module: base64-decodes `content` and writes it
// to `path` on the target — a pure local-filesystem-shaped operation
// (Put/Fetch/Remove/stat through the target Connection, no external
// tool dependency), useful for binary content (not valid UTF-8) that
// ansible.builtin.copy's own `content` parameter cannot carry, since
// that one is defined as a plain string.
//
// Args: path (string, required, alias dest); content (string, required)
// — Base64-encoded; force (bool, default true) — when true, the target
// is overwritten whenever its content differs from the decoded
// `content`; when false, it is written only if it doesn't already
// exist; follow (bool, default false) — when true and path is a
// symlink, the file the symlink resolves to (`readlink -f`) is written
// instead of replacing the symlink itself; backup (bool, default
// false) — before an overwrite, copy the existing file to
// `path.<YYYYMMDDHHMMSS>`; mode (octal string, optional) — chmod'd
// after writing, matching copy.go's own mode handling.
//
// Simplifications vs real write_binary_file: no owner/group/selinux
// (se*)/attributes/unsafe_writes — matching this batch's own house
// convention (see ini_file.go's own doc comment) of not replicating
// ansible.builtin.files' full common file-argument set. backup's
// filename suffix is a plain timestamp, not real write_binary_file's
// own module.backup_local() naming (`<path>.<timestamp>~`).
func moduleWriteBinaryFile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path := argString(args, "path", argString(args, "dest", ""))
	if path == "" {
		return Result{}, errArg("write_binary_file: missing required argument: path (or dest)")
	}
	contentB64, err := requireString(args, "content")
	if err != nil {
		return Result{}, err
	}
	content, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return Result{}, errArg("write_binary_file: cannot decode Base64-encoded content: %v", err)
	}
	force := argBool(args, "force", true)
	backup := argBool(args, "backup", false)
	follow := argBool(args, "follow", false)
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}

	target, err := writeBinaryFileResolvePath(ctx, conn, path, follow)
	if err != nil {
		return Result{}, err
	}

	current, err := fetchIfExists(ctx, conn, target)
	if err != nil {
		return Result{}, err
	}
	if current != nil && !force {
		return Ok("File already exists"), nil
	}

	changed := current == nil || !bytes.Equal(current, content)
	var backupFile string

	if changed {
		if backup && current != nil {
			backupFile = target + "." + time.Now().Format("20060102150405")
			if _, err := run(ctx, conn, "cp "+shellQuote(target)+" "+shellQuote(backupFile)); err != nil {
				return Result{}, err
			}
		}
		if err := writeRemote(ctx, conn, target, content); err != nil {
			return Result{}, err
		}
	}

	if mode != nil {
		info, err := statPath(ctx, conn, target)
		if err != nil {
			return Result{}, err
		}
		if info == nil || info.mode != *mode {
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(target))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	r := Ok(target)
	if changed {
		r = Changed(target)
	}
	if backupFile != "" {
		r = r.WithExtra("backup_file", backupFile)
	}
	return r, nil
}

// writeBinaryFileResolvePath resolves path to the file that should
// actually be written: unchanged unless follow is set and path is
// itself a symlink, in which case it resolves to path's final target
// via `readlink -f`, matching real write_binary_file's own
// `os.path.realpath(path)` when follow and os.path.islink(path).
func writeBinaryFileResolvePath(ctx context.Context, conn remoteexec.Connection, path string, follow bool) (string, error) {
	if !follow {
		return path, nil
	}
	info, err := statPath(ctx, conn, path)
	if err != nil {
		return "", err
	}
	if info == nil || info.kind != fileKindSymlink {
		return path, nil
	}
	out, err := run(ctx, conn, "readlink -f "+shellQuote(path))
	if err != nil {
		return "", err
	}
	resolved := strings.TrimSpace(out)
	if resolved == "" {
		return path, nil
	}
	return resolved, nil
}
