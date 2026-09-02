package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAssemble implements (a subset of) Ansible's `assemble` module:
// concatenates every file fragment found directly under a source
// directory (sorted by name) into one destination file.
//
// Unlike most modules in this package (which operate on data the
// control node already has, or that it moves verbatim via Put/Fetch),
// assemble's whole job is manipulating files that already exist on the
// TARGET — this port therefore composes the listing/filtering/
// concatenation as a shell pipeline over conn.Exec, rather than
// fetching fragments to the control node and reassembling them
// locally only to re-upload the result.
//
// Args: src (string, required) — a directory on the target holding the
// fragments; dest (string, required); regexp (string, optional) — only
// fragment filenames matching this ERE (passed to `grep -E`) are
// included; delimiter (string, optional) — inserted between fragments
// (including after the last one, unlike real assemble, which places it
// only between fragments — see below).
//
// Simplifications vs real ansible.builtin.assemble: no backup, decrypt,
// ignore_hidden, mode/owner/group/attributes, or validate support. Real
// assemble's action plugin can also source fragments from the control
// node (copying them to the target first when they aren't already
// there); this port always assumes src already exists on the target,
// matching the common case this batch's task spec calls out. Real
// assemble is also idempotent — it hashes the assembled content and
// only rewrites dest if it differs (like copy.go's fetch-and-compare
// pattern); this port always rewrites dest and reports changed, since
// composing "assemble remotely into a temp file, fetch it back to
// compare, then conditionally rename" in one round trip added
// complexity this batch didn't budget for — a real gap versus real
// Ansible's idempotent behavior, documented rather than silently
// claimed.
func moduleAssemble(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	src, err := requireString(args, "src")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	regexp := argString(args, "regexp", "")
	delimiter := argString(args, "delimiter", "")

	cmd := assembleCmd(src, dest, regexp, delimiter)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("assemble: %s", strings.TrimSpace(res.Stderr))), nil
	}
	return Changed(dest), nil
}

// assembleCmd builds the list+filter+concatenate shell pipeline for
// moduleAssemble, separated out so its exact shape can be asserted
// directly in tests.
func assembleCmd(src, dest, regexp, delimiter string) string {
	list := "find " + shellQuote(src) + " -mindepth 1 -maxdepth 1 -type f | sort"
	if regexp != "" {
		list += " | grep -E " + shellQuote(regexp)
	}
	body := `cat "$f"`
	if delimiter != "" {
		body += `; printf '%s' ` + shellQuote(delimiter)
	}
	return list + ` | while IFS= read -r f; do ` + body + `; done > ` + shellQuote(dest)
}
