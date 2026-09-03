package modules

import (
	"context"
	"path"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIsoExtract implements (a subset of) Ansible's `iso_extract`
// module: extracts specific files from an ISO image into a destination
// directory, either via `7z` or (if 7z is unavailable) by mounting the
// image — read from real iso_extract.py's own main() (this batch's
// hard rule: the exact 7z-vs-mount fallback and its own unmount-retry
// loop are only visible in the implementation, not EXAMPLES/OPTIONS).
// This one translates directly: real iso_extract already shells out to
// `7z`/`mount`/`umount` itself (it is not pycdlib-based, unlike
// iso_create/iso_customize — see their own doc comments for that
// architectural split).
//
// Args: image (string, required); dest (string, required) — must
// already exist on the target; files ([]string, required) — any
// leading path separator is stripped (matching real `f.lstrip(os.sep)`);
// force (bool, default true) — when false, a file already present at
// dest is left untouched and skipped entirely (not even extracted to
// compare, matching real code's own force=false short-circuit);
// executable (string, default "7z") — path/name of the 7z binary;
// password (string, optional) — passed as `-p<password>` to 7z, the
// same documented "potential security risk" real iso_extract's own
// docs warn about (visible via `ps` to another user on the target
// while 7z runs) — this port does not attempt to hide it either, since
// 7z itself has no password-from-file option to route around that.
//
// Idempotency: real iso_extract compares SHA1 checksums (module.sha1)
// of the extracted temp copy against any existing dest file. This port
// uses `cmp -s` instead (matching decompress.go's own precedent) —
// both files are already on the target, so a target-side byte
// comparison avoids needing a sha1sum/shasum binary at all; functionally
// equivalent for deciding "changed".
//
// 7z-vs-mount fallback: `7z x <image> -o<tmpdir> [-p<password>]
// <files...>` when the executable is found on the target (`-o` glued
// to the path with NO space, matching real 7z's own flag syntax
// exactly); otherwise `mount -o loop,ro <image> <tmpdir>`, with
// `umount <tmpdir>` retried up to 5 times (1-second gaps) afterward —
// both real, documented behaviors, not simplifications.
func moduleIsoExtract(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	image, err := requireString(args, "image")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	files := argStringList(args, "files")
	if len(files) == 0 {
		return Result{}, errArg("iso_extract: files is required and must not be empty")
	}
	force := argBool(args, "force", true)
	executable := argString(args, "executable", "7z")
	password := argString(args, "password", "")

	destExists, err := pathExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	if !destExists {
		return Fail("iso_extract: Directory '" + dest + "' does not exist"), nil
	}
	imageExists, err := pathExists(ctx, conn, image)
	if err != nil {
		return Result{}, err
	}
	if !imageExists {
		return Fail("iso_extract: ISO image '" + image + "' does not exist"), nil
	}

	var extractFiles []string
	var results []map[string]any
	for _, f := range files {
		extractFiles = append(extractFiles, strings.TrimLeft(f, "/"))
	}
	if !force {
		var remaining []string
		for _, f := range extractFiles {
			destFile := dest + "/" + path.Base(f)
			exists, err := pathExists(ctx, conn, destFile)
			if err != nil {
				return Result{}, err
			}
			if exists {
				results = append(results, map[string]any{"src": f, "dest": destFile})
				continue
			}
			remaining = append(remaining, f)
		}
		extractFiles = remaining
	}
	if len(extractFiles) == 0 {
		return Ok("").WithExtra("dest", dest).WithExtra("image", image).WithExtra("files", results), nil
	}

	haveBinary := false
	if _, err := run(ctx, conn, "command -v "+shellQuote(executable)); err == nil {
		haveBinary = true
	}

	tmpDir, err := run(ctx, conn, "mktemp -d")
	if err != nil {
		return Result{}, err
	}

	if haveBinary {
		cmd := shellQuote(executable) + " x " + shellQuote(image) + " -o" + shellQuote(tmpDir)
		if password != "" {
			cmd += " -p" + shellQuote(password)
		}
		for _, f := range extractFiles {
			cmd += " " + shellQuote(f)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			_, _ = runStatus(ctx, conn, "rm -rf "+shellQuote(tmpDir))
			return Fail("iso_extract: failed to extract from ISO image '" + image + "' to '" + tmpDir + "'"), nil
		}
	} else {
		res, err := runStatus(ctx, conn, "mount -o loop,ro "+shellQuote(image)+" "+shellQuote(tmpDir))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			_, _ = runStatus(ctx, conn, "rm -rf "+shellQuote(tmpDir))
			return Fail("iso_extract: failed to mount ISO image '" + image + "' to '" + tmpDir +
				"', and we could not find executable '" + executable + "'."), nil
		}
	}

	changed := false
	failMsg := ""
	for _, f := range extractFiles {
		tmpSrc := tmpDir + "/" + f
		exists, err := pathExists(ctx, conn, tmpSrc)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			failMsg = "iso_extract: Failed to extract '" + f + "' from ISO image"
			break
		}
		destFile := dest + "/" + path.Base(f)
		destExists, err := pathExists(ctx, conn, destFile)
		if err != nil {
			return Result{}, err
		}
		same := false
		if destExists {
			res, err := runStatus(ctx, conn, "cmp -s "+shellQuote(tmpSrc)+" "+shellQuote(destFile))
			if err != nil {
				return Result{}, err
			}
			same = res.RC == 0
		}
		results = append(results, map[string]any{"src": f, "dest": destFile})
		if !same {
			if _, err := run(ctx, conn, "cp -p "+shellQuote(tmpSrc)+" "+shellQuote(destFile)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if !haveBinary {
		for i := 0; i < 5; i++ {
			res, err := runStatus(ctx, conn, "umount "+shellQuote(tmpDir))
			if err == nil && res.RC == 0 {
				break
			}
			if i < 4 {
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
				}
			}
		}
	}
	_, _ = runStatus(ctx, conn, "rm -rf "+shellQuote(tmpDir))

	if failMsg != "" {
		return Fail(failMsg).WithExtra("dest", dest).WithExtra("image", image).WithExtra("files", results), nil
	}

	r := Ok("")
	if changed {
		r = Changed("")
	}
	return r.WithExtra("dest", dest).WithExtra("image", image).WithExtra("files", results), nil
}
