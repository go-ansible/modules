package modules

import (
	"context"
	"fmt"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFile implements (a subset of) Ansible's `file` module: manages
// a path's existence/type and permissions.
//
// Args: path (string, required); state (file|directory|absent|touch|
// link, default "file"); mode (octal string); owner; group; src (the
// link target, for state=link).
func moduleFile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "file")
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}
	owner := argString(args, "owner", "")
	group := argString(args, "group", "")

	changed := false

	switch state {
	case "absent":
		before, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if before == nil {
			return Ok(path + " already absent"), nil
		}
		if _, err := run(ctx, conn, "rm -rf "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		return Changed(path + " removed"), nil

	case "directory":
		before, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if before == nil {
			if _, err := run(ctx, conn, "mkdir -p "+shellQuote(path)); err != nil {
				return Result{}, err
			}
			changed = true
		} else if before.kind != fileKindDir {
			return Fail(fmt.Sprintf("%s exists and is not a directory", path)), nil
		}

	case "touch":
		before, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if _, err := run(ctx, conn, "touch "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		changed = before == nil

	case "link":
		src, err := requireString(args, "src")
		if err != nil {
			return Result{}, err
		}
		before, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if before != nil && before.kind == fileKindSymlink {
			target, err := run(ctx, conn, "readlink "+shellQuote(path))
			if err == nil && target == src {
				break // already the right link
			}
		}
		if _, err := run(ctx, conn, "ln -sfn "+shellQuote(src)+" "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		changed = true

	case "file":
		before, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if before == nil {
			return Fail(fmt.Sprintf("%s does not exist (state=file does not create files — use copy/template/touch)", path)), nil
		}

	default:
		return Result{}, errArg("file: unknown state %q", state)
	}

	if mode != nil {
		before, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if before == nil || before.mode != *mode {
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(path))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if owner != "" || group != "" {
		spec := owner
		if group != "" {
			spec += ":" + group
		}
		if _, err := run(ctx, conn, "chown "+shellQuote(spec)+" "+shellQuote(path)); err != nil {
			return Result{}, err
		}
		// chown's own idempotency isn't probed (no portable owner/group
		// stat format shared across GNU/BSD without extra parsing); it
		// is safe to always issue and let the OS no-op when unchanged,
		// but we can't observe that here, so treat it as changed.
		changed = true
	}

	if changed {
		return Changed(path), nil
	}
	return Ok(path), nil
}
