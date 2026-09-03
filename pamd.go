package modules

import (
	"context"
	"errors"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// pamdNoMatchErr distinguishes "no rule matched" from a genuine
// argument-validation error (like a missing new_type for state=
// before/after): modulePamd converts this one into a Result-level
// Fail rather than a Go error, matching the doc comment's own stated
// intent ("a state other than absent that matches zero rules is a
// Fail") — errArg alone can't express that distinction, since every
// errArg becomes a hard error at the call site.
type pamdNoMatchErr struct{ msg string }

func (e pamdNoMatchErr) Error() string { return e.msg }

func errPamdNoMatch(format string, a ...any) error {
	return pamdNoMatchErr{msg: fmt.Sprintf(format, a...)}
}

// modulePamd implements (a subset of) Ansible's `pamd`
// (community.general) module: edits rule lines in a PAM service file
// under `/etc/pam.d/<name>` (or another directory via `path`). A rule
// line is `type control module_path [module_arguments...]`, where
// `control` may itself be a bracketed, space-containing expression like
// `[success=1 default=ignore]` — this port tokenizes lines with that in
// mind (pamdTokenize), not a plain strings.Fields split.
//
// Args: name (string, required) — the service file's name; path
// (string, default "/etc/pam.d"); backup (bool, default false); type,
// control, module_path (string, all required) — together identify the
// rule(s) a task acts on; new_type, new_control, new_module_path
// (string) — required together for state before/after (the rule being
// inserted); module_arguments ([]string) — meaning depends on state,
// see below; state (updated|before|after|absent|args_absent|
// args_present, default "updated").
//
// State semantics:
//   - updated (default): every existing rule matching type/control/
//     module_path has new_type/new_control/new_module_path applied to
//     whichever of those three were given (a field left unset keeps its
//     current value), and, if module_arguments was given at all (an
//     empty list/string clears them), replaces its arguments entirely.
//   - before / after: inserts a new rule (new_type/new_control/
//     new_module_path, required; module_arguments optional) immediately
//     before/after the FIRST rule matching type/control/module_path.
//     Idempotency is content-based, not position-based: if a rule with
//     the exact same type/control/module_path/arguments already exists
//     ANYWHERE in the file, no insertion happens — this port does not
//     verify it sits in the requested position. Real pamd's own
//     idempotency behavior here was not independently verified; this is
//     a documented, conservative choice that avoids inserting
//     duplicates on a second run.
//   - absent: removes every rule matching type/control/module_path.
//   - args_present / args_absent: for every rule matching type/control/
//     module_path, adds/removes the tokens listed in module_arguments.
//     For args_present, a token containing "=" is matched by its key
//     (the part before "="): an existing argument with the same key has
//     its value replaced; otherwise the token is appended. For
//     args_absent, tokens are removed by exact match only (no key-only
//     matching) — real pamd's own args_absent semantics for "=" tokens
//     were not confirmed from the doc alone, so this port narrows to
//     the unambiguous case.
//
// Every state above (except before/after) applies to ALL matching
// rules, not just one — the ansible-doc text describes matching "an
// existing rule" (singular) but does not say what happens when more
// than one line matches; this port's choice (act on every match) is
// documented here rather than left implicit. A state other than
// `absent` that matches zero rules is a Fail (there is nothing to
// update/insert-relative-to/add-args-to); `absent` matching zero rules
// is a no-op Ok, matching this port's usual "already in the desired
// state" convention.
//
// Simplifications vs real pamd: no authselect-profile awareness (real
// pamd's own doc already disclaims this: "does not handle authselect
// profiles"); a bracketed control's brackets are matched character-for-
// character, not semantically; module_arguments containing a literal
// space are not bracket-quoted on write (real PAM config supports
// `[arg with space]` argument tokens; this port does not generate
// that), which is a narrowing rather than silent data loss, since such
// arguments are simply not expected in the tasks this port targets.
func modulePamd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	typ, err := requireString(args, "type")
	if err != nil {
		return Result{}, err
	}
	control, err := requireString(args, "control")
	if err != nil {
		return Result{}, err
	}
	modulePath, err := requireString(args, "module_path")
	if err != nil {
		return Result{}, err
	}
	path := argString(args, "path", "/etc/pam.d")
	backup := argBool(args, "backup", false)
	state := argString(args, "state", "updated")

	dest := path + "/" + name
	res, err := runStatus(ctx, conn, "cat "+shellQuote(dest)+" 2>/dev/null")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("pamd: %s does not exist", dest)), nil
	}
	lines := splitLines(res.Stdout)

	newLines, changeCount, err := pamdApply(lines, typ, control, modulePath, args, state)
	if err != nil {
		var noMatch pamdNoMatchErr
		if errors.As(err, &noMatch) {
			return Fail(noMatch.msg), nil
		}
		return Result{}, err
	}
	if changeCount == 0 {
		if state == "absent" {
			return Ok(dest + ": no matching rule (already absent)"), nil
		}
		return Ok(dest + ": unchanged"), nil
	}

	var backupDest string
	if backup {
		ts, err := run(ctx, conn, "date +%Y%m%d%H%M%S")
		if err != nil {
			return Result{}, err
		}
		backupDest = dest + "." + ts
		if _, err := run(ctx, conn, "cp "+shellQuote(dest)+" "+shellQuote(backupDest)); err != nil {
			return Result{}, err
		}
	}

	content := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		content += "\n"
	}
	if err := writeRemote(ctx, conn, dest, []byte(content)); err != nil {
		return Result{}, err
	}

	result := Changed(fmt.Sprintf("%s: %d rule(s) changed", dest, changeCount)).WithExtra("change_count", changeCount)
	if backupDest != "" {
		result = result.WithExtra("backupdest", backupDest)
	}
	return result, nil
}

// pamdRule is one parsed (or about-to-be-built) PAM rule line.
type pamdRule struct {
	typ, control, modulePath string
	moduleArgs               []string
}

func (r pamdRule) line() string {
	fields := append([]string{r.typ, r.control, r.modulePath}, r.moduleArgs...)
	return strings.Join(fields, " ")
}

func pamdRuleMatches(r pamdRule, typ, control, modulePath string) bool {
	return r.typ == typ && r.control == control && r.modulePath == modulePath
}

// pamdApply applies state against every line in lines matching
// (typ, control, modulePath), returning the new line set and how many
// rules were changed/inserted/removed.
func pamdApply(lines []string, typ, control, modulePath string, args map[string]any, state string) ([]string, int, error) {
	switch state {
	case "updated":
		return pamdApplyUpdated(lines, typ, control, modulePath, args)
	case "absent":
		return pamdApplyAbsent(lines, typ, control, modulePath)
	case "args_present", "args_absent":
		return pamdApplyArgs(lines, typ, control, modulePath, args, state == "args_present")
	case "before", "after":
		return pamdApplyInsert(lines, typ, control, modulePath, args, state == "before")
	default:
		return nil, 0, errArg("pamd: state must be one of updated, before, after, absent, args_absent, args_present, got %q", state)
	}
}

func pamdApplyUpdated(lines []string, typ, control, modulePath string, args map[string]any) ([]string, int, error) {
	newType := argString(args, "new_type", "")
	newControl := argString(args, "new_control", "")
	newModulePath := argString(args, "new_module_path", "")
	newArgs, argsGiven := pamdModuleArguments(args)

	out := make([]string, len(lines))
	copy(out, lines)
	count := 0
	matched := false
	for i, l := range lines {
		r, ok := pamdParseLine(l)
		if !ok || !pamdRuleMatches(r, typ, control, modulePath) {
			continue
		}
		matched = true
		updated := r
		if newType != "" {
			updated.typ = newType
		}
		if newControl != "" {
			updated.control = newControl
		}
		if newModulePath != "" {
			updated.modulePath = newModulePath
		}
		if argsGiven {
			updated.moduleArgs = newArgs
		}
		newLine := updated.line()
		if newLine != l {
			out[i] = newLine
			count++
		}
	}
	if !matched {
		return nil, 0, errPamdNoMatch("pamd: no rule matching type=%s control=%s module_path=%s found", typ, control, modulePath)
	}
	return out, count, nil
}

func pamdApplyAbsent(lines []string, typ, control, modulePath string) ([]string, int, error) {
	var out []string
	count := 0
	for _, l := range lines {
		r, ok := pamdParseLine(l)
		if ok && pamdRuleMatches(r, typ, control, modulePath) {
			count++
			continue
		}
		out = append(out, l)
	}
	return out, count, nil
}

func pamdApplyArgs(lines []string, typ, control, modulePath string, args map[string]any, present bool) ([]string, int, error) {
	wantArgs, _ := pamdModuleArguments(args)
	out := make([]string, len(lines))
	copy(out, lines)
	count := 0
	matched := false
	for i, l := range lines {
		r, ok := pamdParseLine(l)
		if !ok || !pamdRuleMatches(r, typ, control, modulePath) {
			continue
		}
		matched = true
		var newArgs []string
		if present {
			newArgs = pamdArgsPresent(r.moduleArgs, wantArgs)
		} else {
			newArgs = pamdArgsAbsent(r.moduleArgs, wantArgs)
		}
		r.moduleArgs = newArgs
		newLine := r.line()
		if newLine != l {
			out[i] = newLine
			count++
		}
	}
	if !matched {
		return nil, 0, errPamdNoMatch("pamd: no rule matching type=%s control=%s module_path=%s found", typ, control, modulePath)
	}
	return out, count, nil
}

func pamdApplyInsert(lines []string, typ, control, modulePath string, args map[string]any, before bool) ([]string, int, error) {
	newType, err := requireString(args, "new_type")
	if err != nil {
		return nil, 0, errArg("pamd: new_type is required for state=before/after")
	}
	newControl, err := requireString(args, "new_control")
	if err != nil {
		return nil, 0, errArg("pamd: new_control is required for state=before/after")
	}
	newModulePath, err := requireString(args, "new_module_path")
	if err != nil {
		return nil, 0, errArg("pamd: new_module_path is required for state=before/after")
	}
	newArgs, _ := pamdModuleArguments(args)
	newRule := pamdRule{typ: newType, control: newControl, modulePath: newModulePath, moduleArgs: newArgs}

	for _, l := range lines {
		if r, ok := pamdParseLine(l); ok && pamdRuleMatches(r, newType, newControl, newModulePath) &&
			pamdArgsEqual(r.moduleArgs, newArgs) {
			return lines, 0, nil // already present somewhere: no-op (see doc comment)
		}
	}

	idx := -1
	for i, l := range lines {
		if r, ok := pamdParseLine(l); ok && pamdRuleMatches(r, typ, control, modulePath) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, 0, errPamdNoMatch("pamd: no rule matching type=%s control=%s module_path=%s found to insert %s", typ, control, modulePath, map[bool]string{true: "before", false: "after"}[before])
	}
	insertAt := idx
	if !before {
		insertAt = idx + 1
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, newRule.line())
	out = append(out, lines[insertAt:]...)
	return out, 1, nil
}

func pamdArgsEqual(a, b []string) bool {
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

// pamdModuleArguments resolves the module_arguments argument, returning
// whether it was given at all (an empty string or empty list is "given,
// meaning clear the arguments"; a wholly absent key is "not given,
// meaning leave arguments alone" for state=updated).
func pamdModuleArguments(args map[string]any) ([]string, bool) {
	v, ok := args["module_arguments"]
	if !ok {
		return nil, false
	}
	if s, isStr := v.(string); isStr {
		if s == "" {
			return nil, true
		}
		return strings.Fields(s), true
	}
	return argStringList(args, "module_arguments"), true
}

// pamdArgsPresent adds each of want to existing, replacing the value of
// any existing "key=..." token sharing want's key, or appending
// otherwise (a plain, non-"="-bearing token is appended only if not
// already present).
func pamdArgsPresent(existing, want []string) []string {
	out := append([]string{}, existing...)
	for _, w := range want {
		key, _, hasEq := strings.Cut(w, "=")
		replaced := false
		for i, e := range out {
			if hasEq {
				ekey, _, eHasEq := strings.Cut(e, "=")
				if eHasEq && ekey == key {
					out[i] = w
					replaced = true
					break
				}
			} else if e == w {
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, w)
		}
	}
	return out
}

// pamdArgsAbsent removes every token in existing that exactly matches
// one in remove.
func pamdArgsAbsent(existing, remove []string) []string {
	removeSet := make(map[string]bool, len(remove))
	for _, r := range remove {
		removeSet[r] = true
	}
	var out []string
	for _, e := range existing {
		if !removeSet[e] {
			out = append(out, e)
		}
	}
	return out
}

// pamdParseLine parses one PAM rule line, returning ok=false for a
// blank or comment line.
func pamdParseLine(line string) (pamdRule, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return pamdRule{}, false
	}
	toks := pamdTokenize(trimmed)
	if len(toks) < 3 {
		return pamdRule{}, false
	}
	return pamdRule{typ: toks[0], control: toks[1], modulePath: toks[2], moduleArgs: toks[3:]}, true
}

// pamdTokenize splits a PAM rule line on whitespace, except that a
// "[...]" bracketed control expression (which may itself contain
// spaces) is kept as a single token.
func pamdTokenize(line string) []string {
	var toks []string
	i := 0
	for i < len(line) {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i >= len(line) {
			break
		}
		if line[i] == '[' {
			if j := strings.IndexByte(line[i:], ']'); j >= 0 {
				toks = append(toks, line[i:i+j+1])
				i += j + 1
				continue
			}
		}
		j := i
		for j < len(line) && line[j] != ' ' && line[j] != '\t' {
			j++
		}
		toks = append(toks, line[i:j])
		i = j
	}
	return toks
}
