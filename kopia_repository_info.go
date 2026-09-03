package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKopiaRepositoryInfo implements Ansible's
// `kopia_repository_info` (community.general) module: a read-only
// report of `kopia repository status` and `kopia repository throttle
// get` — read from real kopia_repository_info.py's own
// KopiaRepositoryInfo.__run__.
//
// Args: config (--config-file); password — declared in this module's
// own argument_spec (inherited from KOPIA_COMMON_ARGUMENT_SPEC) but,
// matching real kopia_repository_info's own kopia_runner formats
// ("status config" and "get_throttle config" — neither format string
// includes "password"), never actually passed to either `kopia`
// invocation; this port reproduces that exactly rather than "fixing"
// it by adding --password, since `kopia repository status`/`throttle
// get` do not need repository decryption to answer.
//
// Runs `kopia repository status [--config-file=...]`; a non-zero exit
// fails with "kopia repository status failed with error (rc=N): ...".
// Then runs `kopia repository throttle get [--config-file=...]`; a
// non-zero exit fails the same way with "repository throttle get" in
// place of "repository status" — matching real
// _process_command_output's own cli_action-labeled message exactly.
// Both commands' trimmed stdout (nil if empty, matching real
// `out.rstrip() if out else None`) is returned unconditionally
// changed=false — this module never mutates anything.
//
// Extra: repository_status, throttle (both strings, or omitted from
// Extra entirely when the command's own stdout was empty, mirroring
// real influxdb_query.go's own nil-vs-empty handling elsewhere in this
// port would be overkill here since Ansible's None and Go's absent-Extra-key
// read the same to a caller).
func moduleKopiaRepositoryInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	config := argString(args, "config", "")

	statusOut, err := kopiaInfoRun(ctx, conn, config, "status", []string{"repository", "status"})
	if err != nil {
		return Result{}, err
	}
	if statusOut.failMsg != "" {
		return Fail(statusOut.failMsg), nil
	}

	throttleOut, err := kopiaInfoRun(ctx, conn, config, "repository throttle get", []string{"repository", "throttle", "get"})
	if err != nil {
		return Result{}, err
	}
	if throttleOut.failMsg != "" {
		return Fail(throttleOut.failMsg), nil
	}

	r := Ok("")
	if statusOut.out != "" {
		r = r.WithExtra("repository_status", statusOut.out)
	}
	if throttleOut.out != "" {
		r = r.WithExtra("throttle", throttleOut.out)
	}
	return r, nil
}

type kopiaInfoResult struct {
	out     string
	failMsg string
}

// kopiaInfoRun runs `kopia <argv...> [--config-file=...]` and, on a
// non-zero exit, formats real _process_command_output's own
// "kopia <label> failed with error (rc=N): <err>" message.
func kopiaInfoRun(ctx context.Context, conn remoteexec.Connection, config, label string, argv []string) (kopiaInfoResult, error) {
	if config != "" {
		argv = append(argv, "--config-file="+config)
	}
	res, err := kopiaRun(ctx, conn, argv)
	if err != nil {
		return kopiaInfoResult{}, err
	}
	if res.RC != 0 {
		return kopiaInfoResult{failMsg: fmt.Sprintf("kopia %s failed with error (rc=%d): %s", label, res.RC, strings.TrimSpace(res.Stderr))}, nil
	}
	return kopiaInfoResult{out: strings.TrimSpace(res.Stdout)}, nil
}
