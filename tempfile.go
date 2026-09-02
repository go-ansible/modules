package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleTempfile implements Ansible's `tempfile` module: creates a
// temporary file or directory on the target via `mktemp`, always
// reported as changed (it always creates something new).
//
// Args: state (file|directory, default "file"); path (directory to
// place it in, default "/tmp" — real ansible.builtin.tempfile defaults
// to the system temp directory; conn.TempPath is deliberately not used
// here, since it already builds a control-node-style unique path of its
// own, which doesn't compose with mktemp's XXXXXX templating); prefix
// (default "ansible."); suffix (default "").
//
// The template (dir/prefixXXXXXXsuffix) is passed to mktemp as a plain
// positional argument rather than via -p/--tmpdir: GNU mktemp supports
// -p but BSD/macOS mktemp does not, while a bare template path works
// identically on both.
func moduleTempfile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "file")
	dir := argString(args, "path", "/tmp")
	prefix := argString(args, "prefix", "ansible.")
	suffix := argString(args, "suffix", "")

	cmd, err := tempfileCmd(state, dir, prefix, suffix)
	if err != nil {
		return Result{}, err
	}

	path, err := run(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	return Changed(path).WithExtra("path", path).WithExtra("state", state), nil
}

// tempfileCmd builds the mktemp invocation for moduleTempfile,
// separated out so its exact shape can be asserted directly in tests.
func tempfileCmd(state, dir, prefix, suffix string) (string, error) {
	if state != "file" && state != "directory" {
		return "", errArg("tempfile: state must be file or directory, got %q", state)
	}
	dir = strings.TrimSuffix(dir, "/")
	template := dir + "/" + prefix + "XXXXXX" + suffix
	cmd := "mktemp "
	if state == "directory" {
		cmd += "-d "
	}
	cmd += shellQuote(template)
	return cmd, nil
}
