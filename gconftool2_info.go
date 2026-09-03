package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGconftool2Info implements Ansible's `gconftool2_info`
// (community.general) module: a read-only query of one GConf
// preference key via `gconftool-2 --get` — read from real
// gconftool2_info.py's own GConftoolInfo class and
// module_utils/_gconftool2.py's shared gconftool2_runner (see
// gconftool2.go's own doc comment for that shared arg-format
// background; this module only ever issues the "get" op, never
// "set"/"unset").
//
// Args: key (string, required). Never changes anything (matches real
// gconftool2_info's own read-only nature — this port's Result is
// always Ok, never Changed).
//
// value parsing matches real __run__ exactly, and it is NOT the same
// rule gconftool2.go's own previous_value/value probe uses: value is
// nil only when stderr is non-empty AND stdout is EMPTY; any other
// case (including non-empty stderr alongside non-empty stdout) uses
// the trimmed stdout as-is. A non-zero exit is still a Fail (matching
// the shared runner's own check_rc=True), independent of that stdout/
// stderr rule.
//
// Returns Extra["key"], Extra["value"] (nil for a key with no value
// set), Extra["version"] (`gconftool-2 --version`'s trimmed output,
// always populated, matching real RETURN's own always-returned
// `version`).
func moduleGconftool2Info(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}

	if _, err := run(ctx, conn, "command -v gconftool-2"); err != nil {
		return Fail("gconftool2_info: gconftool-2 executable not found on the target"), nil
	}
	version, err := run(ctx, conn, "gconftool-2 --version")
	if err != nil {
		return Result{}, err
	}

	res, err := runStatus(ctx, conn, "gconftool-2 --get "+shellQuote(key))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("gconftool2_info: gconftool-2 failed with error:\n"+strings.TrimSpace(res.Stderr)).
			WithExtra("key", key).WithExtra("version", version), nil
	}

	var value any
	stdout := strings.TrimRight(res.Stdout, "\n")
	if strings.TrimSpace(res.Stderr) != "" && stdout == "" {
		value = nil
	} else {
		value = stdout
	}

	return Ok("").WithExtra("key", key).WithExtra("value", value).WithExtra("version", version), nil
}
