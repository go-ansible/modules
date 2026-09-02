package modules

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

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

	r := Ok(dest)
	if changed {
		r = Changed(dest)
	}
	return r.WithExtra("dest", dest).WithExtra("src", src), nil
}
