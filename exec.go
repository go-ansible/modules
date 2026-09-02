package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// run executes cmd on conn and returns its trimmed stdout, treating a
// non-zero exit as a Go error (the common case for a module's own
// internal probing commands, where "the command failed" IS "something
// went wrong", unlike a `command`/`shell` task where a non-zero exit is
// the task's own business).
func run(ctx context.Context, conn remoteexec.Connection, cmd string) (string, error) {
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return "", fmt.Errorf("running %q: %w", cmd, err)
	}
	if res.RC != 0 {
		return "", fmt.Errorf("running %q: exit %d: %s", cmd, res.RC, strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout), nil
}

// runStatus is like run but does not treat a non-zero exit as an error
// — for probes whose whole point is the exit code (e.g. `test -e path`).
func runStatus(ctx context.Context, conn remoteexec.Connection, cmd string) (remoteexec.Result, error) {
	return conn.Exec(ctx, cmd, nil)
}

// pathExists reports whether path exists on the target.
func pathExists(ctx context.Context, conn remoteexec.Connection, path string) (bool, error) {
	res, err := runStatus(ctx, conn, "test -e "+shellQuote(path))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
