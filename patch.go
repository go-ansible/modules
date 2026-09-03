package modules

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePatch implements (a subset of) Ansible's `patch` module:
// applies (or reverts) a patch file on the target via the GNU `patch`
// tool.
//
// Args: src (string, required, aliased from patchfile in real patch) —
// a local path (uploaded first via conn.Put) or, when remote_src=true,
// a path already on the target; dest (string, aliased originalfile) or
// basedir (string) — at least one is required, matching real patch's
// own documented requirement; strip (int, default 0) — passed as
// `-p<strip>`; state (present|absent, default "present") — absent adds
// `-R` (reverse/revert); backup (bool, default false) — adds
// `--backup --version-control=numbered`; ignore_whitespace (bool,
// default false) — adds `--ignore-whitespace`.
//
// This module always adds `-N`/`--forward` to the patch invocation,
// which makes a REPEAT application (or reversal) of an already-applied
// patch a silent no-op instead of a hard failure — real GNU patch
// without -N exits non-zero the second time a patch is applied, which
// would make this module non-idempotent on a second playbook run. Real
// ansible.posix.patch achieves the same idempotency by doing its own
// dry-run detection first; this port reaches the same practical
// outcome (a repeat run is safe) via -N instead. Change detection
// itself works differently depending on which of dest/basedir was
// given: when dest is a single file, this port fetches its content
// before and after and compares bytes (byte-for-byte, like copy.go's
// own idempotency check) to decide Changed vs Ok; when only basedir is
// given (a patch that may touch several files named inside the patch
// itself), there is no single file to hash, so this port always
// reports Changed on a zero exit — a real gap versus a byte-accurate
// changed status for the basedir form, documented rather than silently
// claimed (the same tradeoff unarchive.go/assemble.go make for
// analogous reasons).
//
// Simplifications vs real patch: no `binary` (this port never disables
// patch's own CRLF heuristic).
func modulePatch(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	src, err := requireString(args, "src")
	if err != nil {
		return Result{}, err
	}
	dest := argString(args, "dest", "")
	basedir := argString(args, "basedir", "")
	if dest == "" && basedir == "" {
		return Result{}, errArg("patch: one of dest or basedir is required")
	}
	strip := argInt(args, "strip", 0)
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("patch: state must be present or absent, got %q", state)
	}
	backup := argBool(args, "backup", false)
	remoteSrc := argBool(args, "remote_src", false)
	ignoreWhitespace := argBool(args, "ignore_whitespace", false)

	patchPath := src
	if !remoteSrc {
		patchPath = conn.TempPath(filepath.Base(src))
		if err := conn.Put(ctx, src, patchPath, remoteexec.PutOptions{}); err != nil {
			return Result{}, err
		}
	}

	var before []byte
	if dest != "" {
		before, err = fetchIfExists(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
	}

	cmd := patchCmd(patchPath, dest, basedir, strip, backup, ignoreWhitespace, state == "absent")
	res, err := runStatus(ctx, conn, cmd)
	if !remoteSrc {
		_ = conn.Remove(ctx, patchPath) // best-effort cleanup, see script.go's same pattern
	}
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("patch: " + strings.TrimSpace(res.Stderr)), nil
	}

	if dest != "" {
		after, err := fetchIfExists(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
		if bytes.Equal(before, after) {
			return Ok(dest + " already up to date"), nil
		}
		return Changed(dest), nil
	}
	return Changed(basedir), nil
}

// patchCmd builds the `patch` invocation for modulePatch, separated
// out so its exact shape can be asserted directly in tests.
func patchCmd(patchPath, dest, basedir string, strip int, backup, ignoreWhitespace, revert bool) string {
	var b strings.Builder
	if basedir != "" {
		b.WriteString("cd " + shellQuote(basedir) + " && ")
	}
	b.WriteString("patch -N -p" + strconv.Itoa(strip))
	if revert {
		b.WriteString(" -R")
	}
	if backup {
		b.WriteString(" --backup --version-control=numbered")
	}
	if ignoreWhitespace {
		b.WriteString(" --ignore-whitespace")
	}
	if dest != "" {
		b.WriteString(" " + shellQuote(dest))
	}
	b.WriteString(" < " + shellQuote(patchPath))
	return b.String()
}
