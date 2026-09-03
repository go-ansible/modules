package modules

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMake implements Ansible's `make` (community.general) module:
// runs a target in a Makefile via the `make` command.
//
// Args: chdir (string, required) — directory to run `make` from; file
// (string, optional) — passed as `-f`; jobs (int, optional) — passed as
// `-j`; make (string, default "make") — the make binary to invoke; real
// make's own default additionally prefers `gmake` over `make` on
// non-Linux; this port has no target-OS detection to base that
// preference on (see zfs_delegate_admin.go's own doc comment for this
// port's general check_mode/target-introspection convention), so it
// always defaults to bare "make" — documented simplification; params
// (map[string]any, optional) — extra `KEY=VALUE` arguments; a nil value
// emits just `KEY` (matching real make's own doc: "if the value is
// empty, only the key is used"); target (string) XOR targets
// ([]string) — mutually exclusive, matching real make's own
// mutually_exclusive constraint.
//
// Idempotency, exactly matching real make's own approach: this port
// first runs the fully-built command with `-q` appended (GNU make's own
// "question mode": exit 0 means the target is already up to date, no
// output, no side effect) and only re-runs the command for real when
// that check exits non-zero.
//
// Simplification vs real make: `params`' key order is real make's own
// insertion order (Python dict order, following YAML's own key order);
// this port sorts params' keys instead, since a Go map carries no
// order — a cosmetic difference in the exact argv order, not a semantic
// one (each `KEY=VALUE` is independent on make's own command line).
func moduleMake(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	chdir, err := requireString(args, "chdir")
	if err != nil {
		return Result{}, err
	}
	file := argString(args, "file", "")
	makeBin := argString(args, "make", "make")
	target := argString(args, "target", "")
	targets := argStringList(args, "targets")
	if target != "" && len(targets) > 0 {
		return Result{}, errArg("make: target and targets are mutually exclusive")
	}
	params := argMapAny(args, "params")
	_, hasJobs := args["jobs"]
	jobs := argInt(args, "jobs", 0)

	baseCmd := shellQuote(makeBin)
	if hasJobs {
		baseCmd += " -j " + strconv.Itoa(jobs)
	}
	if file != "" {
		baseCmd += " -f " + shellQuote(file)
	}
	if target != "" {
		baseCmd += " " + shellQuote(target)
	} else if len(targets) > 0 {
		baseCmd += " " + quoteAll(targets)
	}
	if len(params) > 0 {
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := params[k]
			if v == nil {
				baseCmd += " " + shellQuote(k)
			} else {
				baseCmd += " " + shellQuote(fmt.Sprintf("%s=%v", k, v))
			}
		}
	}

	fullCmd := "cd " + shellQuote(chdir) + " && " + baseCmd

	checkRes, err := runStatus(ctx, conn, fullCmd+" -q")
	if err != nil {
		return Result{}, err
	}

	result := Ok("target up to date")
	if checkRes.RC != 0 {
		res, err := runStatus(ctx, conn, fullCmd)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(fmt.Sprintf("make: %s", strings.TrimSpace(res.Stderr))), nil
		}
		result = Changed("make ran")
	}

	result = result.WithExtra("chdir", chdir).
		WithExtra("command", baseCmd).
		WithExtra("file", file).
		WithExtra("params", params).
		WithExtra("target", target).
		WithExtra("targets", targets)
	if hasJobs {
		result = result.WithExtra("jobs", jobs)
	} else {
		result = result.WithExtra("jobs", nil)
	}
	return result, nil
}
