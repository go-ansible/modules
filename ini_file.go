package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIniFile implements (a subset of) Ansible's `ini_file` module: a
// section-aware lineinfile.go — it manages one `option = value` line (or
// a whole `[section]`) inside an INI-style file, fetching, editing in Go
// and writing back exactly like lineinfile.go's own fetch/edit/
// writeRemote shape, but tracking which lines fall inside which
// `[section]` block.
//
// Args: path (string, required; aliased from `dest` like real ini_file);
// section (string, default "" — the empty string means "before the
// first section", matching real ini_file's own documented default);
// option (string, default "" — omit to add/remove a whole section
// instead of one option within it); value (string) or values
// ([]string) — mutually exclusive, value=v is equivalent to
// values=[v], as in real ini_file; state (present|absent, default
// "present"); create (bool, default true); backup (bool, default
// false); exclusive (bool, default true) — when true, ALL existing
// lines for `option` are replaced/removed together; when false, only
// the given value(s) are added/removed and other same-named options are
// left alone, matching real ini_file's own documented exclusive
// semantics; allow_no_value (bool, default false) — write a bare
// `option` line with no `=` when no value is given; no_extra_spaces
// (bool, default false) — write `option=value` instead of the default
// `option = value`.
//
// Simplifications vs real ini_file: no `section_has_values` (selecting
// among multiple same-named sections by the options they already
// contain — a materially separate feature, out of scope for this
// batch); no `modify_inactive_option` — this port never treats a
// commented-out line (`#option = value`) as a candidate to replace, the
// same as real ini_file's own modify_inactive_option=false, but that is
// NOT this port's default the way it is real ini_file's (real default
// is true); no `ignore_spaces` — spacing-only differences around `=`
// always count as a change, matching real ini_file's own DEFAULT
// (ignore_spaces=false) but with no way to opt into the looser
// comparison; option-name matching is a plain case-sensitive string
// comparison — real ini_file, via Python's configparser, lowercases
// option names by default (configparser's `optionxform`), which this
// port does not replicate; no `follow`/mode/owner/group/attributes/
// selinux(se*)/unsafe_writes.
func moduleIniFile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := iniFileRequirePath(args)
	if err != nil {
		return Result{}, err
	}
	section := argString(args, "section", "")
	option := argString(args, "option", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ini_file: state must be present or absent, got %q", state)
	}
	backup := argBool(args, "backup", false)
	create := argBool(args, "create", true)
	exclusive := argBool(args, "exclusive", true)
	allowNoValue := argBool(args, "allow_no_value", false)
	noExtraSpaces := argBool(args, "no_extra_spaces", false)
	values, hasValue := iniFileValues(args)

	current, err := fetchIfExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	existed := current != nil
	if current == nil {
		if !create {
			return Fail(fmt.Sprintf("%s does not exist (set create: true to allow creating it)", path)), nil
		}
		current = []byte{}
	}
	lines := splitLines(string(current))

	var newLines []string
	var changed bool
	if option == "" {
		newLines, changed = iniFileApplySection(lines, section, state)
	} else {
		if !hasValue && state == "present" && !allowNoValue {
			return Result{}, errArg("ini_file: value (or values) is required when state is present, unless allow_no_value is set")
		}
		desired := iniFileDesiredLines(option, values, hasValue, allowNoValue, noExtraSpaces)
		newLines, changed = iniFileApplyOption(lines, section, option, desired, state, exclusive, hasValue)
	}

	if !changed {
		return Ok(path + " unchanged"), nil
	}

	if backup && existed {
		if _, err := run(ctx, conn, "cp "+shellQuote(path)+" "+shellQuote(path)+".$(date +%Y%m%d%H%M%S) 2>/dev/null"); err != nil {
			return Result{}, err
		}
	}

	newContent := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		newContent += "\n"
	}
	if err := writeRemote(ctx, conn, path, []byte(newContent)); err != nil {
		return Result{}, err
	}
	return Changed(path), nil
}

// iniFileRequirePath resolves the `path` argument, falling back to its
// real-ini_file alias `dest`.
func iniFileRequirePath(args map[string]any) (string, error) {
	if s, ok := args["path"].(string); ok && s != "" {
		return s, nil
	}
	if s, ok := args["dest"].(string); ok && s != "" {
		return s, nil
	}
	return "", errArg("ini_file: path (or its alias dest) is required")
}

// iniFileValues resolves the `value`/`values` arguments (mutually
// exclusive per real ini_file), reporting whether either was given at
// all so a caller can distinguish "no value given" from "given an empty
// string".
func iniFileValues(args map[string]any) (values []string, has bool) {
	if _, ok := args["values"]; ok {
		return argStringList(args, "values"), true
	}
	if v, ok := args["value"]; ok {
		if s, ok2 := v.(string); ok2 {
			return []string{s}, true
		}
		return []string{fmt.Sprint(v)}, true
	}
	return nil, false
}

func iniFileSectionHeader(name string) string { return "[" + name + "]" }

// iniFileIsSectionHeader reports whether line, once trimmed, is a
// `[section]` header, returning its name.
func iniFileIsSectionHeader(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if len(t) >= 2 && t[0] == '[' && t[len(t)-1] == ']' {
		return t[1 : len(t)-1], true
	}
	return "", false
}

// iniFileSectionBounds returns the [start,end) line-index range of
// section's body: the line right after its `[section]` header (start)
// through the line right before the next section header or EOF (end).
// section="" means the file's preamble, from line 0 up to (not
// including) the first section header. Returns (-1,-1) if a named
// section isn't found.
func iniFileSectionBounds(lines []string, section string) (start, end int) {
	if section == "" {
		end = len(lines)
		for i, l := range lines {
			if _, ok := iniFileIsSectionHeader(l); ok {
				end = i
				break
			}
		}
		return 0, end
	}
	start = -1
	for i, l := range lines {
		if name, ok := iniFileIsSectionHeader(l); ok && name == section {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if _, ok := iniFileIsSectionHeader(lines[i]); ok {
			end = i
			break
		}
	}
	return start, end
}

// iniFileOptionKey extracts an option line's key (the text before its
// first "=", or the whole trimmed line for a bare allow_no_value-style
// option), reporting false for a blank line, a comment (# or ;), or a
// section header — none of which are option lines.
func iniFileOptionKey(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
		return "", false
	}
	if _, ok := iniFileIsSectionHeader(t); ok {
		return "", false
	}
	if idx := strings.IndexByte(t, '='); idx >= 0 {
		return strings.TrimSpace(t[:idx]), true
	}
	return t, true
}

// iniFileDesiredLines builds the literal line(s) that should exist for
// option once state=present, or nil if there is no fixed set of
// content to compare against (state=absent with no value given, which
// removes by key alone — see iniFileApplyOption).
func iniFileDesiredLines(option string, values []string, hasValue, allowNoValue, noExtraSpaces bool) []string {
	if !hasValue {
		if allowNoValue {
			return []string{option}
		}
		return nil
	}
	sep := " = "
	if noExtraSpaces {
		sep = "="
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = option + sep + v
	}
	return out
}

// iniFileApplySection adds or removes a whole [section] (option==""
// case). section=="" is a no-op in both directions: the file's preamble
// always conceptually exists and can't be "removed" as a block.
func iniFileApplySection(lines []string, section, state string) ([]string, bool) {
	if section == "" {
		return lines, false
	}
	start, end := iniFileSectionBounds(lines, section)
	if state == "present" {
		if start >= 0 {
			return lines, false
		}
		out := append(append([]string{}, lines...), iniFileSectionHeader(section))
		return out, true
	}
	// state == "absent"
	if start < 0 {
		return lines, false
	}
	out := append(append([]string{}, lines[:start]...), lines[end:]...)
	return out, true
}

// iniFileApplyOption adds, replaces, or removes option's line(s) within
// section (or the preamble, if section==""), per state/exclusive.
func iniFileApplyOption(lines []string, section, option string, desired []string, state string, exclusive, hasValue bool) ([]string, bool) {
	work := append([]string{}, lines...)
	start, end := iniFileSectionBounds(work, section)
	if start < 0 {
		if state != "present" || section == "" {
			return lines, false
		}
		work = append(work, iniFileSectionHeader(section))
		start, end = len(work), len(work)
	}

	var matched []int
	for i := start; i < end; i++ {
		if key, ok := iniFileOptionKey(work[i]); ok && key == option {
			matched = append(matched, i)
		}
	}

	if state == "present" {
		if exclusive {
			if len(matched) == 0 {
				out := append([]string{}, work[:end]...)
				out = append(out, desired...)
				out = append(out, work[end:]...)
				return out, true
			}
			if len(matched) == 1 && len(desired) == 1 && work[matched[0]] == desired[0] {
				return lines, false
			}
			out := append([]string{}, work...)
			insertPos := matched[0]
			for i := len(matched) - 1; i >= 0; i-- {
				out = append(out[:matched[i]], out[matched[i]+1:]...)
			}
			tail := append([]string{}, out[insertPos:]...)
			out = append(out[:insertPos:insertPos], desired...)
			out = append(out, tail...)
			return out, true
		}
		existing := map[string]bool{}
		for _, i := range matched {
			existing[work[i]] = true
		}
		var toAdd []string
		for _, d := range desired {
			if !existing[d] {
				toAdd = append(toAdd, d)
			}
		}
		if len(toAdd) == 0 {
			return lines, false
		}
		out := append([]string{}, work[:end]...)
		out = append(out, toAdd...)
		out = append(out, work[end:]...)
		return out, true
	}

	// state == "absent"
	var toRemove []int
	if exclusive || !hasValue {
		toRemove = matched
	} else {
		want := map[string]bool{}
		for _, d := range desired {
			want[d] = true
		}
		for _, i := range matched {
			if want[work[i]] {
				toRemove = append(toRemove, i)
			}
		}
	}
	if len(toRemove) == 0 {
		return lines, false
	}
	out := append([]string{}, work...)
	for i := len(toRemove) - 1; i >= 0; i-- {
		out = append(out[:toRemove[i]], out[toRemove[i]+1:]...)
	}
	return out, true
}
