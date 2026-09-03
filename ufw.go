package modules

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUfw implements Ansible's `ufw` (community.general) module:
// composes and runs one `ufw` CLI invocation per requested command
// (state/default/logging/rule), then derives Changed by comparing
// `ufw status verbose` output (and the currently-installed user rules,
// via grep over ufw's own rule files) from before to after — read from
// real ufw.py's own main(), since its idempotency strategy for a real
// (non-check-mode) run is not documented anywhere but the source: real
// ufw.py ALWAYS runs its ufw command for real and only afterwards
// diffs pre/post status+rules text to decide changed (its own
// elaborate dry-run/regex-matching code paths are check_mode-only,
// which this port has no equivalent of, so this is the whole
// algorithm this port needs — not a narrowed subset of it).
//
// Args (at least one of state/default/logging/rule is required, and
// this port processes them in that fixed order if more than one is
// given, matching real ufw's own command_keys iteration order):
//
//   - state (enabled|disabled|reloaded|reset) — `ufw -f
//     enable|disable|reload|reset`; reloaded/reset always report
//     Changed unconditionally (matching real ufw's own
//     `if value in ("reloaded","reset"): changed = True`, set BEFORE
//     even running the command).
//   - logging (on|off|low|medium|high|full) — `ufw logging <value>`.
//   - default (allow|deny|reject, aliased policy) — `ufw default
//     <value> [direction]`; direction, if given, must be one of
//     incoming, outgoing, routed.
//   - rule (allow|deny|limit|reject) — the long-form `ufw [route]
//     [delete|insert N] <rule> [in|out] [on IFACE | in on IFACE_IN
//     [out on IFACE_OUT]] [log] from <from_ip> [port <from_port>] to
//     <to_ip> [port <to_port>] [proto P] [app 'NAME'] [comment 'C']`.
//     from_ip/to_ip default to "any" (matching real ufw's own default,
//     which means "from any"/"to any" appear in the composed command
//     even when neither is set explicitly); direction, if given, must
//     be "in" or "out"; interface is mutually exclusive with
//     interface_in/interface_out and requires direction (matching real
//     ufw's own required_by); interface_in+interface_out together
//     require route=true; comment is only appended when the target's
//     installed `ufw --version` is >= 0.35 (matching real ufw's own
//     version gate — silently dropped, not an error, on an older ufw,
//     exactly as real ufw itself does); insert (with insert_relative_to,
//     default "zero") selects a rule position, computed against `ufw
//     status numbered`'s own listing for the non-"zero" relative modes,
//     matching real ufw's own arithmetic (see ufwInsertPosition).
//
// Simplifications vs real ufw: no check_mode (there is no dry-run
// distinct path to implement — see above); this port validates only
// the mutual-exclusion/required-by relationships real ufw's own
// argspec enforces for the fields this module actually implements
// (interface/interface_in/interface_out/direction, route for the
// interface_in+interface_out combination); it does not replicate real
// ufw's own top-level `mutually_exclusive=[["name","proto","logging"]]`
// group, since name/proto only ever apply to the "rule" command and
// logging is a wholly separate command in this port's per-command
// dispatch, making that particular exclusivity moot here.
func moduleUfw(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := run(ctx, conn, "command -v ufw >/dev/null 2>&1 && command -v grep >/dev/null 2>&1"); err != nil {
		return Result{}, err
	}

	preState, err := run(ctx, conn, "ufw status verbose")
	if err != nil {
		return Result{}, err
	}
	preRules, err := ufwCurrentRules(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	var cmds []string
	forcedChanged := false

	if state := argString(args, "state", ""); state != "" {
		states := map[string]string{"enabled": "enable", "disabled": "disable", "reloaded": "reload", "reset": "reset"}
		verb, ok := states[state]
		if !ok {
			return Result{}, errArg("ufw: state must be one of enabled, disabled, reloaded, reset, got %q", state)
		}
		if state == "reloaded" || state == "reset" {
			forcedChanged = true
		}
		cmd := "ufw -f " + verb
		if _, failRes := runAndRecord(ctx, conn, cmd, &cmds); failRes != nil {
			return *failRes, nil
		}
	}

	if def := argString(args, "default", ""); def != "" {
		direction := argString(args, "direction", "")
		if direction != "" && direction != "incoming" && direction != "outgoing" && direction != "routed" {
			return Result{}, errArg(`ufw: for default, direction must be one of "incoming", "outgoing", "routed", or unset`)
		}
		cmd := "ufw default " + def
		if direction != "" {
			cmd += " " + direction
		}
		if _, failRes := runAndRecord(ctx, conn, cmd, &cmds); failRes != nil {
			return *failRes, nil
		}
	}

	if logging := argString(args, "logging", ""); logging != "" {
		cmd := "ufw logging " + logging
		if _, failRes := runAndRecord(ctx, conn, cmd, &cmds); failRes != nil {
			return *failRes, nil
		}
	}

	if rule := argString(args, "rule", ""); rule != "" {
		cmd, err := ufwBuildRuleCmd(ctx, conn, rule, args)
		if err != nil {
			return Result{}, err
		}
		if cmd == "" {
			return Fail("ufw: could not build rule command"), nil
		}
		if _, res := runAndRecord(ctx, conn, cmd, &cmds); res != nil {
			return *res, nil
		}
	}

	if len(cmds) == 0 {
		return Result{}, errArg("ufw: one of state, default, logging, rule is required")
	}

	postState, err := run(ctx, conn, "ufw status verbose")
	if err != nil {
		return Result{}, err
	}
	postRules, err := ufwCurrentRules(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	changed := forcedChanged || preState != postState || preRules != postRules
	result := Ok(strings.TrimRight(postState, "\n"))
	if changed {
		result = Changed(strings.TrimRight(postState, "\n"))
	}
	return result.WithExtra("commands", cmds), nil
}

// runAndRecord runs cmd, appends it to *cmds regardless of outcome
// (matching real ufw's own `cmds` list, which records every attempted
// command for the final failure/success report), and returns a non-nil
// *Result only on failure (a normal Result{Failed:true}, not a Go
// error, matching real ufw's own module.fail_json(msg=err or out) on
// any single command's non-zero exit).
func runAndRecord(ctx context.Context, conn remoteexec.Connection, cmd string, cmds *[]string) (string, *Result) {
	*cmds = append(*cmds, cmd)
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		r := Fail(err.Error())
		return "", &r
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		r := Fail(msg).WithExtra("commands", *cmds)
		return "", &r
	}
	return res.Stdout, nil
}

// ufwCurrentRules greps ufw's own installed user-rules files for their
// "### tuple" marker lines — the same signal real ufw's own
// get_current_rules() uses to detect an actual ruleset change, since
// `ufw status` alone doesn't reliably reflect every rule edit.
func ufwCurrentRules(ctx context.Context, conn remoteexec.Connection) (string, error) {
	files := []string{
		"/lib/ufw/user.rules", "/lib/ufw/user6.rules",
		"/etc/ufw/user.rules", "/etc/ufw/user6.rules",
		"/var/lib/ufw/user.rules", "/var/lib/ufw/user6.rules",
	}
	cmd := "grep -h '^### tuple'"
	for _, f := range files {
		cmd += " " + shellQuote(f)
	}
	cmd += " 2>/dev/null"
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// ufwBuildRuleCmd composes the long-form `ufw rule` command line for
// the "rule" command.
func ufwBuildRuleCmd(ctx context.Context, conn remoteexec.Connection, rule string, args map[string]any) (string, error) {
	direction := argString(args, "direction", "")
	if direction != "" && direction != "in" && direction != "out" {
		return "", errArg(`ufw: for rules, direction must be one of "in", "out", or unset`)
	}
	route := argBool(args, "route", false)
	delete_ := argBool(args, "delete", false)
	interfaceArg := argString(args, "interface", "")
	interfaceIn := argString(args, "interface_in", "")
	interfaceOut := argString(args, "interface_out", "")
	if interfaceArg != "" && (interfaceIn != "" || interfaceOut != "") {
		return "", errArg("ufw: interface is mutually exclusive with interface_in and interface_out")
	}
	if interfaceArg != "" && direction == "" {
		return "", errArg("ufw: interface requires direction to be set")
	}
	if !route && interfaceIn != "" && interfaceOut != "" {
		return "", errArg("ufw: only route rules can combine interface_in and interface_out")
	}
	log := argBool(args, "log", false)
	fromIP := argString(args, "from_ip", "any")
	fromPort := argString(args, "from_port", "")
	toIP := argString(args, "to_ip", "any")
	toPort := argString(args, "to_port", "")
	proto := argString(args, "proto", "")
	name := argString(args, "name", "")
	comment := argString(args, "comment", "")

	var b strings.Builder
	b.WriteString("ufw ")
	if route {
		b.WriteString("route ")
	}
	if delete_ {
		b.WriteString("delete ")
	} else if _, ok := args["insert"]; ok {
		insert := argInt(args, "insert", 0)
		relTo := argString(args, "insert_relative_to", "zero")
		insertTo, omit, err := ufwInsertPosition(ctx, conn, insert, relTo)
		if err != nil {
			return "", err
		}
		if !omit {
			b.WriteString("insert " + strconv.Itoa(insertTo) + " ")
		}
	}
	b.WriteString(rule + " ")
	if direction != "" {
		b.WriteString(direction + " ")
	}
	if interfaceArg != "" {
		b.WriteString("on " + interfaceArg + " ")
	}
	if interfaceIn != "" {
		b.WriteString("in on " + interfaceIn + " ")
	}
	if interfaceOut != "" {
		b.WriteString("out on " + interfaceOut + " ")
	}
	if log {
		b.WriteString("log ")
	}
	b.WriteString("from " + fromIP + " ")
	if fromPort != "" {
		b.WriteString("port " + fromPort + " ")
	}
	b.WriteString("to " + toIP + " ")
	if toPort != "" {
		b.WriteString("port " + toPort + " ")
	}
	if proto != "" {
		b.WriteString("proto " + proto + " ")
	}
	if name != "" {
		b.WriteString("app '" + name + "' ")
	}
	if comment != "" {
		major, minor, _, err := ufwVersion(ctx, conn)
		if err != nil {
			return "", err
		}
		if major > 0 || (major == 0 && minor >= 35) {
			b.WriteString("comment '" + comment + "' ")
		}
	}
	return strings.TrimSpace(b.String()), nil
}

var ufwVersionRe = regexp.MustCompile(`^ufw.+?(\d+)\.(\d+)(?:\.(\d+))?`)

// ufwVersion returns the installed `ufw --version`'s major/minor/patch.
func ufwVersion(ctx context.Context, conn remoteexec.Connection) (major, minor, rev int, err error) {
	out, err := run(ctx, conn, "ufw --version")
	if err != nil {
		return 0, 0, 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := ufwVersionRe.FindStringSubmatch(line)
		if m == nil {
			return 0, 0, 0, errArg("ufw: failed to get ufw version from %q", line)
		}
		major, _ = strconv.Atoi(m[1])
		minor, _ = strconv.Atoi(m[2])
		if m[3] != "" {
			rev, _ = strconv.Atoi(m[3])
		}
		return major, minor, rev, nil
	}
	return 0, 0, 0, errArg("ufw: failed to get ufw version (no output)")
}

var ufwNumberedRe = regexp.MustCompile(`^\[\s*([0-9]+)\]\s`)

// ufwInsertPosition computes the absolute rule number real ufw.py's own
// `insert` handling would pass to `ufw insert N`, given insert and
// insert_relative_to. omit=true means real ufw.py would drop the
// "insert N" token entirely (relative_to_cmd resolved to a position
// past the last known rule).
func ufwInsertPosition(ctx context.Context, conn remoteexec.Connection, insert int, relativeTo string) (insertTo int, omit bool, err error) {
	if relativeTo == "zero" {
		return insert, false, nil
	}
	out, err := run(ctx, conn, "ufw status numbered")
	if err != nil {
		return 0, false, err
	}
	var lastNumber int
	hasIPv4, hasIPv6 := false, false
	var lastIPv4 int
	for _, line := range strings.Split(out, "\n") {
		m := ufwNumberedRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		no, _ := strconv.Atoi(m[1])
		if no > lastNumber {
			lastNumber = no
		}
		if strings.Contains(line, "(v6)") {
			hasIPv6 = true
		} else {
			hasIPv4 = true
			if no > lastIPv4 {
				lastIPv4 = no
			}
		}
	}
	var rel int
	switch relativeTo {
	case "first-ipv4":
		rel = 1
	case "last-ipv4":
		if hasIPv4 {
			rel = lastIPv4
		} else {
			rel = 1
		}
	case "first-ipv6":
		if hasIPv4 {
			rel = lastIPv4 + 1
		} else {
			rel = 1
		}
	case "last-ipv6":
		if hasIPv6 {
			rel = lastNumber
		} else {
			rel = lastNumber + 1
		}
	default:
		return 0, false, errArg("ufw: insert_relative_to must be one of zero, first-ipv4, last-ipv4, first-ipv6, last-ipv6, got %q", relativeTo)
	}
	insertTo = insert + rel
	if insertTo > lastNumber {
		return 0, true, nil
	}
	return insertTo, false, nil
}
