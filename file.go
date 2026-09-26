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

	// Under check mode every mutation below becomes a no-op while the
	// surrounding decisions run unchanged, so the module reports exactly
	// what it would have done. Routing all of them through one helper is
	// what makes that auditable: a mutation that forgot to use it would
	// stand out, where scattered `if check` guards would not.
	check := InCheckMode(args)
	mutate := func(cmd string) error {
		if check {
			return nil
		}
		_, err := run(ctx, conn, cmd)
		return err
	}

	changed := false

	switch state {
	case "absent":
		before, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if before == nil {
			return Result{NoMsg: true, Extra: map[string]any{
				"path": path, "state": "absent",
			}}, nil
		}
		if err := mutate("rm -rf " + shellQuote(path)); err != nil {
			return Result{}, err
		}
		// Real reports no msg for file at all -- what a playbook
		// registers is the path and the resulting state. This port
		// returned the path AS the msg, so `r.msg` read back a
		// filename where real gives "absent".
		return Result{Changed: true, NoMsg: true, Extra: map[string]any{
			"path": path, "state": "absent",
		}}, nil

	case "directory":
		before, err := statPath(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if before == nil {
			if err := mutate("mkdir -p " + shellQuote(path)); err != nil {
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
		if err := mutate("touch " + shellQuote(path)); err != nil {
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
		if err := mutate("ln -sfn " + shellQuote(src) + " " + shellQuote(path)); err != nil {
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
			if err := mutate(fmt.Sprintf("chmod %04o %s", *mode, shellQuote(path))); err != nil {
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
		if err := mutate("chown " + shellQuote(spec) + " " + shellQuote(path)); err != nil {
			return Result{}, err
		}
		// chown's own idempotency isn't probed (no portable owner/group
		// stat format shared across GNU/BSD without extra parsing); it
		// is safe to always issue and let the OS no-op when unchanged,
		// but we can't observe that here, so treat it as changed.
		changed = true
	}

	// state=touch is the one that reports the path under "dest";
	// every other state uses "path". Measured, and a playbook reading
	// the wrong one gets nothing.
	pathKey := "path"
	if state == "touch" {
		pathKey = "dest"
	}
	return fileResult(ctx, conn, path, pathKey, changed)
}

// fileResult describes the path as it stands AFTER the module ran, in
// the shape real's file module returns: no msg, and the ownership and
// mode a playbook reads back from a registered result. This port
// returned only changed/failed/msg, so `r.state`, `r.mode` and the
// rest were simply absent and every reference to them failed.
func fileResult(ctx context.Context, conn remoteexec.Connection, path, pathKey string, changed bool) (Result, error) {
	after, err := statPath(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	extra := map[string]any{pathKey: path}
	if after == nil {
		// Nothing there to describe -- a dry run that created
		// nothing, for one. Real has a real file to stat at this
		// point; saying only what is known beats inventing the rest.
		extra["state"] = "absent"
		return Result{Changed: changed, NoMsg: true, Extra: extra}, nil
	}
	extra["state"] = fileStateName(after)
	extra["mode"] = fmt.Sprintf("0%o", after.mode)
	extra["size"] = after.size
	extra["uid"] = after.uid
	extra["gid"] = after.gid
	extra["owner"] = after.owner
	extra["group"] = after.group
	return Result{Changed: changed, NoMsg: true, Extra: extra}, nil
}

// fileStateName is real's name for what a path IS, which is not
// always the state that was asked for: state=touch reports "file".
func fileStateName(fi *fileInfo) string {
	switch fi.kind {
	case fileKindDir:
		return "directory"
	case fileKindSymlink:
		return "link"
	default:
		return "file"
	}
}
