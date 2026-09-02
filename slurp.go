package modules

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSlurp implements Ansible's `slurp` module: reads a file from
// the target and returns its content base64-encoded — always base64,
// matching real ansible.builtin.slurp, since the content may be binary
// and the transport in real Ansible is JSON (this port doesn't share
// that constraint, but keeps the same contract for a caller expecting
// it).
//
// Args: src (string, required; `path` accepted as an alias, matching
// real ansible.builtin.slurp).
func moduleSlurp(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	src := argString(args, "src", argString(args, "path", ""))
	if src == "" {
		return Result{}, errArg("slurp: missing required argument: src")
	}

	exists, err := pathExists(ctx, conn, src)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail(fmt.Sprintf("%s does not exist", src)), nil
	}

	tmp, err := os.CreateTemp("", "go-ansible-slurp-*")
	if err != nil {
		return Result{}, fmt.Errorf("slurp: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := conn.Fetch(ctx, src, tmpPath); err != nil {
		return Result{}, fmt.Errorf("slurp: fetching %s: %w", src, err)
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return Result{}, fmt.Errorf("slurp: %w", err)
	}

	r := Ok(src)
	r = r.WithExtra("content", base64.StdEncoding.EncodeToString(data))
	r = r.WithExtra("encoding", "base64")
	return r, nil
}
