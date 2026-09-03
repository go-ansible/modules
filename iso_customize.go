package modules

import (
	"context"
	"path/filepath"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIsoCustomize implements (a subset of) Ansible's `iso_customize`
// module: adds/replaces and removes files inside an existing ISO image,
// writing the result to a (typically different) destination path —
// read from real iso_customize.py's own iso_rebuild/iso_add_file/
// iso_delete_file pycdlib calls (this batch's hard rule: the exact
// per-file-type iso9660/rr/joliet/udf branching pycdlib needs is only
// visible in the implementation, not EXAMPLES/OPTIONS).
//
// Architectural deviation, and why: exactly like iso_create.go (see its
// own doc comment for the fuller rationale), real iso_customize links
// pycdlib directly and has no external CLI command to read off. This
// port shells out to `xorriso` instead, using its native (non-mkisofs-
// emulation) modify workflow: `-indev <src_iso> -outdev <dest_iso>
// -rm_r <paths...> -- -map <local> <iso_path> ... -commit` — verified
// against an actual installed xorriso 1.5.8 (create, then add+delete in
// one indev/outdev invocation, then list contents) rather than guessed.
// The `--` after -rm_r's own path list is load-bearing: -rm_r consumes
// every following token as another path to remove until it hits `--`
// (confirmed directly — without it, xorriso misparses the next `-map`
// flag as a removal target and errors); deletes are issued before adds,
// matching real iso_rebuild's own delete-then-add order.
//
// Args: src_iso (string, required); dest_iso (string, required) —
// dest_iso's own parent directory must already exist (matching real
// main()'s own check, which fails rather than mkdir -p'ing it, unlike
// iso_create's own dest_iso handling); delete_files ([]string, default
// []); add_files ([]map{src_file,dest_file}, default []) — src_file is
// itself a target-side path (matching this port's own "reach the
// target only through Connection" architecture, same deviation
// iso_extract.go and archive.go already document); at least one of
// delete_files/add_files must be non-empty (matching real
// required_one_of).
//
// NOT reproduced: real iso_customize's own Rock-Ridge-1.09/1.10
// workaround (rm_file then add_file, because pycdlib's own add_file
// with rr_name doesn't overwrite under old Rock Ridge revisions — see
// its own module doc note) is pycdlib-internal plumbing this port's
// xorriso-based -map already sidesteps entirely: xorriso's own -map
// always overwrites an existing path regardless of what extensions the
// source ISO carries, so there is no equivalent workaround to
// replicate — a genuine case of the substitute tool not needing the
// same caveat, not a gap.
//
// Idempotency: none — matching real iso_customize's own unconditional
// `result["changed"] = True` every run, this port always executes and
// always reports Changed.
func moduleIsoCustomize(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	srcISO, err := requireString(args, "src_iso")
	if err != nil {
		return Result{}, err
	}
	destISO, err := requireString(args, "dest_iso")
	if err != nil {
		return Result{}, err
	}
	deleteFiles := argStringList(args, "delete_files")
	type addFile struct{ src, dest string }
	var addFiles []addFile
	if raw, ok := args["add_files"].([]any); ok {
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				return Result{}, errArg("iso_customize: add_files entries must be objects with src_file/dest_file")
			}
			src, err := requireString(m, "src_file")
			if err != nil {
				return Result{}, errArg("iso_customize: add_files: %v", err)
			}
			dest, err := requireString(m, "dest_file")
			if err != nil {
				return Result{}, errArg("iso_customize: add_files: %v", err)
			}
			addFiles = append(addFiles, addFile{src, dest})
		}
	}
	if len(deleteFiles) == 0 && len(addFiles) == 0 {
		return Result{}, errArg("iso_customize: at least one of delete_files or add_files is required")
	}

	srcExists, err := pathExists(ctx, conn, srcISO)
	if err != nil {
		return Result{}, err
	}
	if !srcExists {
		return Fail("iso_customize: ISO file " + srcISO + " does not exist."), nil
	}
	destDir := filepath.Dir(destISO)
	if destDir != "" && destDir != "." {
		exists, err := pathExists(ctx, conn, destDir)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail("iso_customize: The dest directory " + destDir + " does not exist"), nil
		}
	}
	for _, af := range addFiles {
		exists, err := pathExists(ctx, conn, af.src)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail("iso_customize: The file " + af.src + " does not exist."), nil
		}
	}

	var b strings.Builder
	b.WriteString("xorriso -indev " + shellQuote(srcISO) + " -outdev " + shellQuote(destISO))
	if len(deleteFiles) > 0 {
		b.WriteString(" -rm_r")
		for _, f := range deleteFiles {
			b.WriteString(" " + shellQuote(isoAbsPath(f)))
		}
		b.WriteString(" --")
	}
	for _, af := range addFiles {
		b.WriteString(" -map " + shellQuote(af.src) + " " + shellQuote(isoAbsPath(af.dest)))
	}
	b.WriteString(" -commit")

	if _, err := run(ctx, conn, b.String()); err != nil {
		return Result{}, err
	}

	addFilesExtra := make([]map[string]any, len(addFiles))
	for i, af := range addFiles {
		addFilesExtra[i] = map[string]any{"src_file": af.src, "dest_file": af.dest}
	}

	return Changed("").
		WithExtra("src_iso", srcISO).
		WithExtra("dest_iso", destISO).
		WithExtra("delete_files", deleteFiles).
		WithExtra("add_files", addFilesExtra), nil
}

// isoAbsPath ensures p starts with "/", matching real iso_customize's
// own `if dest_file[0] != "/": dest_file = f"/{dest_file}"` normalization.
func isoAbsPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}
