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
// It is idempotent, as real is: the fragments are assembled into a
// temporary file, compared with the destination, and only moved into
// place when they differ. Writing straight to the destination reported
// "changed" on every run -- measured against real, which reports ok on
// the second -- so a handler notified by an assemble fired every time.
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

	// Real refuses a missing source in its own words and leaves the
	// destination untouched -- measured: "Source (<path>) does not
	// exist", and no file created. Checked explicitly because the
	// wrapper below succeeds whatever find did: relying on the pipeline's
	// exit status made a missing source produce an EMPTY destination.
	exists, err := pathExists(ctx, conn, src)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail(fmt.Sprintf("Source (%s) does not exist", src)), nil
	}

	// Assemble into a temporary file, compare, and move only on a
	// difference. The marker tells the two apart in one round trip.
	// assembleCmd shell-quotes its destination, which is right for a
	// path and wrong for a shell variable: passing "$__asm_tmp" through
	// it wrote to a file literally called $__asm_tmp and left the real
	// one empty. The redirection is appended here instead.
	inner := assemblePipeline(src, regexp, delimiter) + ` > "$__asm_tmp"`
	cmd := `__asm_tmp=$(mktemp) || exit 1
` + inner + `
if cmp -s "$__asm_tmp" ` + shellQuote(dest) + ` 2>/dev/null; then
  rm -f "$__asm_tmp"; printf 'ASSEMBLE_UNCHANGED\n'
else
  mv "$__asm_tmp" ` + shellQuote(dest) + ` && printf 'ASSEMBLE_CHANGED\n'
fi`
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("assemble: %s", strings.TrimSpace(res.Stderr))), nil
	}
	changed := strings.Contains(res.Stdout, "ASSEMBLE_CHANGED")

	out := Ok("")
	if changed {
		out = Changed("")
	}
	out = out.WithExtra("src", src).WithExtra("dest", dest)
	sums, err := fileSums(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	for k, v := range sums {
		out = out.WithExtra(k, v)
	}
	return out, nil
}

// fileSums reports what real's assemble reports about the file it wrote:
// its sha1 under "checksum", its md5 under "md5sum", and the ownership
// and mode the stat dictionary already knows. Measured against real,
// whose key set is changed, checksum, dest, failed, gid, group, md5sum,
// mode, msg, owner, size, src, state, uid.
func fileSums(ctx context.Context, conn remoteexec.Connection, path string) (map[string]any, error) {
	dict, err := statDict(ctx, conn, path, statOptions{GetChecksum: true})
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, k := range []string{"gid", "mode", "size", "uid", "checksum"} {
		if v, ok := dict[k]; ok {
			out[k] = v
		}
	}
	// The stat dictionary spells these pw_name and gr_name, as stat(1)
	// does; real's assemble reports them as owner and group.
	if v, ok := dict["pw_name"]; ok {
		out["owner"] = v
	}
	if v, ok := dict["gr_name"]; ok {
		out["group"] = v
	}
	out["state"] = "file"
	res, err := conn.Exec(ctx, "md5 -q "+shellQuote(path)+" 2>/dev/null || md5sum "+shellQuote(path)+" 2>/dev/null | awk '{print $1}'", nil)
	if err == nil {
		out["md5sum"] = strings.TrimSpace(res.Stdout)
	}
	return out, nil
}

// assembleCmd builds the list+filter+concatenate shell pipeline for
// moduleAssemble, separated out so its exact shape can be asserted
// directly in tests.
func assembleCmd(src, dest, regexp, delimiter string) string {
	return assemblePipeline(src, regexp, delimiter) + " > " + shellQuote(dest)
}

// assemblePipeline is assembleCmd without the redirection, so the output
// can be sent somewhere a shell-quoted path cannot name -- a temporary
// file held in a shell variable.
func assemblePipeline(src, regexp, delimiter string) string {
	list := "find " + shellQuote(src) + " -mindepth 1 -maxdepth 1 -type f | sort"
	if regexp != "" {
		// Real matches the regular expression against the FILE NAME,
		// as re.search on os.listdir's entries. Grepping the whole line
		// matched it against the full path instead, so an anchored
		// pattern like ^0[13]- selected nothing at all -- find prints
		// /dir/01-a, whose start is the directory.
		list += " | awk -v __re=" + shellQuote(regexp) + " -F/ '$NF ~ __re'"
	}
	body := `cat "$f"`
	if delimiter != "" {
		body += `; printf '%s' ` + shellQuote(delimiter)
	}
	return list + ` | while IFS= read -r f; do ` + body + `; done`
}
