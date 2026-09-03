package modules

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIptablesState implements (a subset of) Ansible's
// `iptables_state` (community.general) module: saves the live
// iptables/ip6tables ruleset to a file (via `iptables-save`), or
// restores one from a file (via `iptables-restore`) — distinct from
// this port's firewalld.go, which drives firewalld's own D-Bus-backed
// zone/rule model rather than raw iptables state. Read from real
// iptables_state.py's own main()/filter_and_format_state/
// parse_per_table_state (this batch's hard rule: its idempotency
// comparison — filtering timestamps and, unless counters=true, zeroing
// packet/byte counters before comparing dumps — isn't documented in
// EXAMPLES/RETURN VALUES).
//
// Args: path (path, required); state (saved|restored, required);
// table (filter|nat|mangle|raw|security, optional) — restricts both
// the save/restore to one table (passed as `--table` to
// iptables-save/-restore, matching real iptables_state's own COMMANDARGS,
// which are shared between the save and restore commands); noflush
// (bool, default false) — for state=restored, passed as
// `iptables-restore --noflush` (don't flush tables not mentioned in
// the input first); counters (bool, default false) — save/restore
// real packet/byte counter VALUES instead of zeroing them; when false,
// this port (like real iptables_state) zeroes every `[N:M]` counter
// pair in a dump before comparing/writing it, so counter churn alone
// never causes a spurious Changed; modprobe (path, optional) — passed
// as `--modprobe <path>` to both save and restore; ip_version
// (ipv4|ipv6, default "ipv4") — selects iptables/iptables-save/
// iptables-restore vs their ip6tables- equivalents; wait (int,
// optional) — passed as `--wait <N>` to the restore/test commands.
//
// state=saved: runs `iptables-save [--counters] [--table T]
// [--modprobe M]`, and writes its (timestamp-stripped,
// counter-zeroed-unless-counters) output to path if that differs from
// path's current content (a byte-exact comparison, matching real
// iptables_state's own write_state).
//
// state=restored: fails cleanly if path doesn't exist, or (when table
// is given) if path doesn't contain a `*<table>` section; otherwise
// test-restores via `iptables-restore --test` first (failing cleanly,
// without touching the live ruleset, on a bad ruleset file), then
// actually restores via `iptables-restore [--noflush] <path>` — path is
// passed as iptables-restore's OWN input-file argument, so it reads the
// file directly from the target's filesystem rather than this port
// streaming its content over stdin. Changed is computed by comparing
// each table's post-restore ruleset (via a fresh `iptables-save`) to
// its pre-restore snapshot, table by table — matching real
// iptables_state's own comparison, which is per-table content equality,
// not a single global before/after string diff.
//
// NOT implemented (a real, deliberate narrowing, not a hidden gap):
//   - The async rollback safety net real iptables_state's own
//     action plugin provides (its own `_timeout`/`_back` private args,
//     documented in its own NOTES as requiring `async`+`poll: 0` task
//     attributes): if a bad restore locks this port out of the target,
//     nothing here brings it back automatically. Real iptables_state's
//     rollback is implemented at the ACTION PLUGIN layer (running on
//     the controller, racing an async task on the target) — a
//     wholly different execution model than this package's Func
//     signature, matching mail.go/synchronize.go's own precedent for
//     documenting an architectural gap honestly rather than faking a
//     subset.
//   - The "initialize from a completely virgin table" bootstrap dance
//     real iptables_state's own initialize_from_null_state performs
//     (writing a throwaway `*table / :OUTPUT ACCEPT / COMMIT` ruleset
//     first) for a table that has NEVER been touched since boot, so
//     that a later rollback has something to restore to. This port
//     always saves/restores against iptables-save's own current
//     output directly; a table iptables-save doesn't mention at all
//     yet is simply absent from `tables`/`initial_state`, matching
//     ordinary iptables-save behavior for an untouched table.
//   - Real iptables_state's own per-table `iptables-restore --test`
//     loop (a workaround for an nft --test bug restoring multiple
//     tables at once, https://bugs.debian.org/960003): this port
//     issues one `--test` call covering whatever `table` restricts it
//     to (or the whole file otherwise).
func moduleIptablesState(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "saved" && state != "restored" {
		return Result{}, errArg("iptables_state: state must be saved or restored, got %q", state)
	}
	table := argString(args, "table", "")
	if table != "" {
		valid := map[string]bool{"filter": true, "nat": true, "mangle": true, "raw": true, "security": true}
		if !valid[table] {
			return Result{}, errArg("iptables_state: table must be one of filter, nat, mangle, raw, security, got %q", table)
		}
	}
	noflush := argBool(args, "noflush", false)
	counters := argBool(args, "counters", false)
	modprobe := argString(args, "modprobe", "")
	ipVersion := argString(args, "ip_version", "ipv4")
	if ipVersion != "ipv4" && ipVersion != "ipv6" {
		return Result{}, errArg("iptables_state: ip_version must be ipv4 or ipv6, got %q", ipVersion)
	}
	wait := argInt(args, "wait", 0)
	_, hasWait := args["wait"]

	saveBin, restoreBin := "iptables-save", "iptables-restore"
	if ipVersion == "ipv6" {
		saveBin, restoreBin = "ip6tables-save", "ip6tables-restore"
	}
	if _, err := run(ctx, conn, "command -v "+saveBin+" >/dev/null 2>&1 && command -v "+restoreBin+" >/dev/null 2>&1"); err != nil {
		return Fail(saveBin + "/" + restoreBin + " not found on PATH"), nil
	}

	commandArgs := ""
	if counters {
		commandArgs += " --counters"
	}
	if table != "" {
		commandArgs += " --table " + table
	}
	if modprobe != "" {
		commandArgs += " --modprobe " + shellQuote(modprobe)
	}
	saveCmd := saveBin + commandArgs

	if state == "restored" {
		content, err := fetchIfExists(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if content == nil {
			return Fail("Source " + path + " not found"), nil
		}
		if table != "" {
			found := false
			for _, l := range strings.Split(string(content), "\n") {
				if strings.TrimSpace(l) == "*"+table {
					found = true
					break
				}
			}
			if !found {
				return Fail("Table " + table + " to restore not defined in " + path), nil
			}
		}
	}

	initOut, err := run(ctx, conn, saveCmd)
	if err != nil {
		return Result{}, err
	}
	tablesBefore := iptablesParsePerTable(initOut, counters)
	initialState := iptablesFilterFormat(initOut, counters)

	if state == "saved" {
		newContent := ""
		if len(initialState) > 0 {
			newContent = strings.Join(initialState, "\n") + "\n"
		}
		existing, err := fetchIfExists(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		changed := existing == nil || string(existing) != newContent
		if changed {
			if err := writeRemote(ctx, conn, path, []byte(newContent)); err != nil {
				return Result{}, err
			}
		}
		result := Ok(path)
		if changed {
			result = Changed(path)
		}
		return result.WithExtra("cmd", saveCmd).WithExtra("tables", tablesBefore).
			WithExtra("initial_state", initialState).WithExtra("saved", initialState), nil
	}

	waitFlag := ""
	if hasWait {
		waitFlag = " --wait " + strconv.Itoa(wait)
	}
	testCmd := restoreBin + " --test" + commandArgs + waitFlag + " " + shellQuote(path)
	testRes, err := runStatus(ctx, conn, testCmd)
	if err != nil {
		return Result{}, err
	}
	if testRes.RC != 0 {
		msg := "Source " + path + " is not suitable for input to " + restoreBin
		if strings.Contains(testRes.Stderr, "Another app is currently holding the xtables lock") {
			msg = testRes.Stderr
		}
		return Fail(msg).WithExtra("cmd", testCmd).WithExtra("tables", tablesBefore).
			WithExtra("initial_state", initialState).WithExtra("applied", false), nil
	}

	restoreCmd := restoreBin + commandArgs + waitFlag
	if noflush {
		restoreCmd += " --noflush"
	}
	restoreCmd += " " + shellQuote(path)
	mainRes, err := runStatus(ctx, conn, restoreCmd)
	if err != nil {
		return Result{}, err
	}
	if strings.Contains(mainRes.Stderr, "Another app is currently holding the xtables lock") {
		return Fail(mainRes.Stderr).WithExtra("cmd", restoreCmd).WithExtra("tables", tablesBefore).
			WithExtra("initial_state", initialState).WithExtra("applied", false), nil
	}
	if mainRes.RC != 0 {
		return Fail(strings.TrimSpace(mainRes.Stderr)).WithExtra("cmd", restoreCmd).
			WithExtra("tables", tablesBefore).WithExtra("initial_state", initialState).
			WithExtra("applied", false), nil
	}

	afterOut, err := run(ctx, conn, saveCmd)
	if err != nil {
		return Result{}, err
	}
	tablesAfter := iptablesParsePerTable(afterOut, counters)
	restoredState := iptablesFilterFormat(afterOut, counters)

	changed := false
	for t, content := range tablesAfter {
		before, ok := tablesBefore[t]
		if !ok || !iptablesEqualLines(before, content) {
			changed = true
			break
		}
	}

	result := Ok(path)
	if changed {
		result = Changed(path)
	}
	return result.WithExtra("cmd", restoreCmd).WithExtra("tables", tablesBefore).
		WithExtra("initial_state", initialState).WithExtra("restored", restoredState).
		WithExtra("applied", true), nil
}

var (
	iptablesTimestampRe = regexp.MustCompile(`((^|\n)# (Generated|Completed)[^\n]*) on [^\n]*`)
	iptablesCounterRe   = regexp.MustCompile(`\[[0-9]+:[0-9]+\]`)
	iptablesTableHdrRe  = regexp.MustCompile(`^\*(filter|mangle|nat|raw|security)$`)
)

// iptablesFilterFormat strips "# Generated/Completed ... on <date>"
// timestamp suffixes (for run-to-run idempotence) and, unless counters
// is true, zeroes every `[N:M]` packet/byte counter pair, then splits
// into non-empty lines — matching real iptables_state's own
// filter_and_format_state.
func iptablesFilterFormat(dump string, counters bool) []string {
	s := iptablesTimestampRe.ReplaceAllString(dump, "$1")
	if !counters {
		s = iptablesCounterRe.ReplaceAllString(s, "[0:0]")
	}
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// iptablesParsePerTable groups a filtered dump's lines by their
// enclosing `*table`/`COMMIT` section, matching real iptables_state's
// own parse_per_table_state.
func iptablesParsePerTable(dump string, counters bool) map[string][]string {
	lines := iptablesFilterFormat(dump, counters)
	tables := map[string][]string{}
	currentTable := ""
	var current []string
	for _, l := range lines {
		if m := iptablesTableHdrRe.FindStringSubmatch(l); m != nil {
			currentTable = m[1]
			continue
		}
		if l == "COMMIT" {
			tables[currentTable] = current
			currentTable, current = "", nil
			continue
		}
		if strings.HasPrefix(l, "# ") {
			continue
		}
		current = append(current, l)
	}
	return tables
}

func iptablesEqualLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
