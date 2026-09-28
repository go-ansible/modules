package modules

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFetch implements Ansible's `fetch` module: copies a file from
// the target to the control node, idempotently (skips overwriting dest
// when its content already matches the fetched bytes).
//
// Real ansible.builtin.fetch lays files out under dest/hostname/src,
// keyed by inventory_hostname, because one control-node run fans out
// over many target hosts. This port's module signature has no
// inventory/hostname concept threaded through it — a module only sees
// one Connection at a time, with nothing identifying which inventory
// host it is — so dest is treated as a literal local file path,
// matching how copy's src/dest already work in this port. A caller
// that wants the per-host tree can build dest itself before invoking
// this module.
//
// Args: src (string, required, remote path); dest (string, required,
// local path); fail_on_missing (bool, default true).
func moduleFetch(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	src, err := requireString(args, "src")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	failOnMissing := argBool(args, "fail_on_missing", true)

	exists, err := pathExists(ctx, conn, src)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		msg := fmt.Sprintf("%s does not exist", src)
		if failOnMissing {
			return Fail(msg), nil
		}
		return Ok(msg), nil
	}

	tmp, err := os.CreateTemp("", "go-ansible-fetch-*")
	if err != nil {
		return Result{}, fmt.Errorf("fetch: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := conn.Fetch(ctx, src, tmpPath); err != nil {
		return Result{}, fmt.Errorf("fetch: fetching %s: %w", src, err)
	}
	newData, err := os.ReadFile(tmpPath)
	if err != nil {
		return Result{}, fmt.Errorf("fetch: %w", err)
	}

	// A dest ending in a separator names a DIRECTORY, and the file
	// lands in it under its own basename -- which is what `flat: true`
	// with a trailing slash means. Treating it as a file name failed
	// with "is a directory".
	if strings.HasSuffix(dest, string(os.PathSeparator)) {
		dest = filepath.Join(dest, filepath.Base(src))
	}

	changed := true
	if oldData, err := os.ReadFile(dest); err == nil && bytes.Equal(oldData, newData) {
		changed = false
	}

	if changed {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return Result{}, fmt.Errorf("fetch: %w", err)
		}
		if err := os.WriteFile(dest, newData, 0o644); err != nil {
			return Result{}, fmt.Errorf("fetch: writing %s: %w", dest, err)
		}
	}

	// Real's own keys, measured:
	//
	//	changed    changed checksum dest failed file md5sum
	//	           remote_checksum remote_md5sum
	//	unchanged  the same without the two remote_ ones
	//
	// dest is the RESOLVED destination file, file is the remote source,
	// and remote_md5sum is empty -- real stopped filling it and kept the
	// key. This port reported dest and src, and src is not one of them.
	r := Ok("")
	if changed {
		r = Changed("")
	}
	r.NoMsg = true
	sum := sha1.Sum(newData)
	checksum := hex.EncodeToString(sum[:])
	md5 := md5.Sum(newData)
	r = r.WithExtra("dest", dest).
		WithExtra("file", src).
		WithExtra("checksum", checksum).
		WithExtra("md5sum", hex.EncodeToString(md5[:]))
	if changed {
		r = r.WithExtra("remote_checksum", checksum).
			WithExtra("remote_md5sum", "")
	}
	return r, nil
}
