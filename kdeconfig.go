package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKdeconfig implements (a subset of) Ansible's `kdeconfig`
// (community.general) module: adds or changes settings in a KDE
// configuration file via `kwriteconfig` — read from real kdeconfig.py's
// own run_module/run_kwriteconfig functions (this batch's hard rule:
// the temp-file-then-atomic-move shape and per-row argv construction
// are only visible there, not EXAMPLES/OPTIONS).
//
// Args: path (string, required) — created if it does not already
// exist; kwriteconfig_path (string) — an explicit executable path,
// required=true if given (matching real kdeconfig's own
// `get_bin_path(required=True)` for an explicit path); if omitted,
// this port probes kwriteconfig6, kwriteconfig5, kwriteconfig, then
// kwriteconfig4 in that order (real kdeconfig's own documented
// discovery order), and fails cleanly if none are found; values
// ([]map, required) — each entry needs exactly one of group/groups
// (a single group name, or a list building a nested group path via
// repeated `--group` flags) and exactly one of value/bool_value, plus
// a non-empty key.
//
// Each value row runs its own `kwriteconfig --file <tmp> --key <key>
// [--group <g>]... (--type bool true|false | -- <value>)` against a
// TARGET-SIDE temporary copy of the file (conn.TempPath, seeded with
// the existing file's content if any, matching real kdeconfig's own
// local temp-file-then-atomic_move shape); after all rows have run,
// the temp file is compared byte-for-byte against the ORIGINAL content
// this module started with (also matching real kdeconfig — it computes
// changed from a before/after text diff of the whole file, not from
// each kwriteconfig call's own exit status) and, if different (or the
// file didn't exist yet), moved into place at path via `mv`.
//
// Simplifications vs real kdeconfig: no owner/group/mode/attributes/
// SELinux context/unsafe_writes/backup support (this port never
// chowns/chmods a file it writes, see blockinfile.go's own
// simplifications list for the same narrowing elsewhere in this
// package); no diff_mode.
func moduleKdeconfig(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	rows, err := kdeconfigParseValues(args)
	if err != nil {
		return Result{}, err
	}
	if len(rows) == 0 {
		return Result{}, errArg("kdeconfig: missing required argument: values")
	}
	for _, r := range rows {
		if strings.TrimSpace(r.key) == "" {
			return Fail("kdeconfig: 'key' cannot be empty"), nil
		}
	}

	kwriteconfig, failMsg, err := kdeconfigFindBinary(ctx, conn, args)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	existed, before, err := kdeconfigReadExisting(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}

	tmp := conn.TempPath("kdeconfig")
	if _, err := conn.Exec(ctx, "cat > "+shellQuote(tmp), strings.NewReader(before)); err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.Remove(ctx, tmp) }()

	for _, r := range rows {
		cmd := kdeconfigCmd(kwriteconfig, tmp, r)
		res, err := runStatus(ctx, conn, cmd)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("kdeconfig: "+strings.TrimSpace(res.Stderr)).WithExtra("path", path), nil
		}
	}

	// Compare the RAW (untrimmed) before/after content — real kdeconfig
	// computes changed from a byte-for-byte diff of the whole file, not
	// from any individual kwriteconfig call's own exit status.
	changed := !existed
	if existed {
		afterRes, err := runStatus(ctx, conn, "cat "+shellQuote(tmp))
		if err != nil {
			return Result{}, err
		}
		changed = afterRes.Stdout != before
	}

	if !changed {
		return Ok("OK").WithExtra("path", path), nil
	}
	if _, err := run(ctx, conn, "mv "+shellQuote(tmp)+" "+shellQuote(path)); err != nil {
		return Result{}, err
	}
	return Changed("OK").WithExtra("path", path), nil
}

type kdeconfigValue struct {
	groups  []string
	key     string
	value   string
	isBool  bool
	boolVal bool
}

// kdeconfigParseValues validates and extracts the `values` argument's
// rows, matching real kdeconfig's own suboption constraints (one of
// group/groups, one of value/bool_value, key required).
func kdeconfigParseValues(args map[string]any) ([]kdeconfigValue, error) {
	raw, ok := args["values"]
	if !ok {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, errArg("kdeconfig: values must be a list")
	}
	rows := make([]kdeconfigValue, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, errArg("kdeconfig: values[%d] must be a dict", i)
		}
		var groups []string
		if g := argString(m, "group", ""); g != "" {
			groups = []string{g}
		}
		if gs := argStringList(m, "groups"); len(gs) > 0 {
			groups = gs
		}
		if len(groups) == 0 {
			return nil, errArg("kdeconfig: values[%d]: one of group or groups is required", i)
		}
		key := argString(m, "key", "")
		row := kdeconfigValue{groups: groups, key: key}
		if _, hasBool := m["bool_value"]; hasBool {
			row.isBool = true
			row.boolVal = argBool(m, "bool_value", false)
		} else if v, hasVal := m["value"]; hasVal {
			row.value = fmt.Sprint(v)
		} else {
			return nil, errArg("kdeconfig: values[%d]: one of value or bool_value is required", i)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// kdeconfigFindBinary resolves the kwriteconfig executable: an explicit
// kwriteconfig_path (required to exist), or the first of kwriteconfig6/
// kwriteconfig5/kwriteconfig/kwriteconfig4 found on PATH.
func kdeconfigFindBinary(ctx context.Context, conn remoteexec.Connection, args map[string]any) (bin string, failMsg string, err error) {
	if explicit := argString(args, "kwriteconfig_path", ""); explicit != "" {
		if _, err := run(ctx, conn, "command -v "+shellQuote(explicit)); err != nil {
			return "", "kdeconfig: " + explicit + " not found on the target", nil
		}
		return explicit, "", nil
	}
	for _, name := range []string{"kwriteconfig6", "kwriteconfig5", "kwriteconfig", "kwriteconfig4"} {
		res, err := runStatus(ctx, conn, "command -v "+name+" >/dev/null 2>&1")
		if err != nil {
			return "", "", err
		}
		if res.RC == 0 {
			return name, "", nil
		}
	}
	return "", "kdeconfig: kwriteconfig is not installed", nil
}

// kdeconfigReadExisting reads path's current content, if any (existed
// is false if the file does not exist, matching real kdeconfig's own
// "OSError -> changed" fallback rather than treating it as an error).
func kdeconfigReadExisting(ctx context.Context, conn remoteexec.Connection, path string) (existed bool, content string, err error) {
	exists, err := pathExists(ctx, conn, path)
	if err != nil {
		return false, "", err
	}
	if !exists {
		return false, "", nil
	}
	res, err := runStatus(ctx, conn, "cat "+shellQuote(path))
	if err != nil {
		return false, "", err
	}
	if res.RC != 0 {
		return false, "", nil
	}
	return true, res.Stdout, nil
}

// kdeconfigCmd builds one row's `kwriteconfig` invocation, matching
// real run_kwriteconfig's own argv order: --file, --key, then a
// --group flag per group (in order — kwriteconfig interprets repeated
// --group flags as a nested group path), then either "--type bool
// true|false" or "-- <value>".
func kdeconfigCmd(bin, tmp string, r kdeconfigValue) string {
	cmd := shellQuote(bin) + " --file " + shellQuote(tmp) + " --key " + shellQuote(r.key)
	for _, g := range r.groups {
		cmd += " --group " + shellQuote(g)
	}
	if r.isBool {
		if r.boolVal {
			cmd += " --type bool true"
		} else {
			cmd += " --type bool false"
		}
	} else {
		cmd += " -- " + shellQuote(r.value)
	}
	return cmd
}
