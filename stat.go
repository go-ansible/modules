package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleStat implements Ansible's `stat` module: reports whether a path
// exists and, if so, its size/mode/type. Never changes anything.
//
// Args: path (string, required).
func moduleStat(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	info, err := statPath(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if info == nil {
		return Ok("").WithExtra("stat", map[string]any{"exists": false}), nil
	}
	return Ok("").WithExtra("stat", map[string]any{
		"exists": true,
		"size":   info.size,
		"mode":   fmt.Sprintf("%04o", info.mode),
		"isdir":  info.kind == fileKindDir,
		"islnk":  info.kind == fileKindSymlink,
		"isreg":  info.kind == fileKindRegular,
		"path":   path,
	}), nil
}

type fileKind int

const (
	fileKindRegular fileKind = iota
	fileKindDir
	fileKindSymlink
	fileKindOther
)

type fileInfo struct {
	size int64
	mode uint32
	kind fileKind
}

// statPath probes path on the target with GNU stat syntax, falling back
// to BSD stat (macOS/*BSD) if that fails, normalizing both into one
// shape. Returns (nil, nil) if the path does not exist.
func statPath(ctx context.Context, conn remoteexec.Connection, path string) (*fileInfo, error) {
	q := shellQuote(path)
	cmd := fmt.Sprintf(
		"stat -c '%%s|%%a|%%F' %s 2>/dev/null || stat -f '%%z|%%Lp|%%HT' %s 2>/dev/null",
		q, q,
	)
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	out := strings.TrimSpace(res.Stdout)
	if res.RC != 0 || out == "" {
		return nil, nil // does not exist
	}
	parts := strings.SplitN(out, "|", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("stat %s: unexpected stat output %q", path, out)
	}
	size, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("stat %s: parsing size %q: %w", path, parts[0], err)
	}
	mode, err := strconv.ParseUint(parts[1], 8, 32)
	if err != nil {
		return nil, fmt.Errorf("stat %s: parsing mode %q: %w", path, parts[1], err)
	}
	kind := fileKindOther
	switch t := strings.ToLower(parts[2]); {
	case strings.Contains(t, "directory"):
		kind = fileKindDir
	case strings.Contains(t, "symbolic link"):
		kind = fileKindSymlink
	case strings.Contains(t, "regular"):
		kind = fileKindRegular
	}
	return &fileInfo{size: size, mode: uint32(mode), kind: kind}, nil
}
