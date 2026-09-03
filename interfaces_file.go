package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleInterfacesFile implements Ansible's `interfaces_file`
// (community.general) module: reads and (optionally) edits ONE option
// within ONE `iface` stanza of a Debian-style `/etc/network/interfaces`
// file, in place — read from real interfaces_file.py's own
// read_interfaces_lines/set_interface_option/addOptionAfterLine
// (this batch's hard rule: its exact stanza-boundary and in-place-edit
// rules aren't visible from EXAMPLES/RETURN VALUES alone).
//
// Args: dest (path, default "/etc/network/interfaces"); iface (string)
// — required whenever option is given (this port returns an argument
// error, not a Result, for that combination — an argspec-level
// requirement, matching real interfaces_file's own `required_by`);
// address_family (string, optional) — narrows which `iface <name>
// <family> ...` stanza(s) an edit or lookup applies to, when the same
// iface name has more than one (e.g. inet + inet6); option, value
// (string) — the option to add/update/remove and its value (value is
// required when option is given and state=present, matching real
// interfaces_file's own validation); state (present|absent, default
// "present"); backup (bool, default false).
//
// Stanza parsing matches real interfaces_file's own state machine
// exactly: a line starting a new stanza (iface/mapping/source/
// source-dir/source-directory/auto/allow-*/no-auto-down/no-scripts)
// ends the PREVIOUS iface stanza; a blank line or comment does NOT
// (real interfaces_file's own read loop only resets state on a
// recognized keyword line — a real, easy-to-miss detail this port
// replicates faithfully rather than "fixing").
//
// Editing an existing option updates it in place at its current
// position (searching for its own previously-parsed value as a literal
// substring of its own raw line and splicing in the new one, so
// indentation and any trailing inline comment on that line survive);
// adding a new option inserts a new line right after the stanza's last
// existing option (or right after the `iface` line itself, 4-space
// indented, if it has none yet) — matching real interfaces_file's own
// addOptionAfterLine, including one real quirk: pre-up/up/down/post-up
// are treated as repeatable (a given value is added again unless that
// EXACT value already exists; state=absent with a value only removes
// matching-value entries), while every other option name is
// single-valued (state=present edits the LAST matching line in place;
// state=absent removes ALL matching lines). Editing "method" is a
// special case in real interfaces_file (and here): method is a
// positional field of the `iface` line itself, not a separate option
// line, so setting option="method" rewrites the matching `iface`
// line(s)' own trailing method token instead of adding a new line.
//
// Return value: ifaces (map) — mirrors real interfaces_file's own
// RETURN VALUE of the same name: one entry per iface name, each with
// address_family/method/pre-up/up/down/post-up plus any other options
// set. Faithfully replicated real quirk: if the SAME iface name has
// multiple stanzas in the file, only the LAST one's data survives in
// this summary (real interfaces_file's own currif dict is simply
// overwritten each time a new `iface <name>` line is parsed) — the
// underlying file lines for earlier stanzas are untouched either way,
// only this derived fact map loses them.
//
// Simplifications vs real interfaces_file: no owner/group/mode/
// attributes/selinux(se*)/unsafe_writes/validate (this port has no
// remote chown/chmod primitive to apply them through); dest must
// already exist — real interfaces_file's own open() would raise an
// uncaught, ungraceful Python traceback for a missing file (it has no
// `create` option), which this port instead reports as a clean
// Result{Failed:true}, a practically-equivalent outcome (the task
// fails either way) without literally reproducing a traceback.
func moduleInterfacesFile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dest := argString(args, "dest", "/etc/network/interfaces")
	iface := argString(args, "iface", "")
	addressFamily := argString(args, "address_family", "")
	option := argString(args, "option", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("interfaces_file: state must be present or absent, got %q", state)
	}
	backup := argBool(args, "backup", false)
	valueStr := argString(args, "value", "")
	_, hasValue := args["value"]

	if option != "" && iface == "" {
		return Result{}, errArg("interfaces_file: iface is required when option is set")
	}
	if option != "" && state == "present" && !hasValue {
		return Result{}, errArg("interfaces_file: value must be set if option is defined and state is 'present'")
	}

	current, err := fetchIfExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	if current == nil {
		return Fail(dest + " does not exist"), nil
	}

	entries := interfacesParseLines(string(current))

	changed := false
	if option != "" {
		var failRes *Result
		changed, entries, failRes = interfacesSetOption(entries, iface, option, valueStr, state, addressFamily)
		if failRes != nil {
			return *failRes, nil
		}
	}

	ifaces := interfacesBuildFacts(entries)

	if changed {
		if backup {
			if _, err := run(ctx, conn, "cp "+shellQuote(dest)+" "+shellQuote(dest)+".$(date +%Y%m%d%H%M%S) 2>/dev/null"); err != nil {
				return Result{}, err
			}
		}
		if err := writeRemote(ctx, conn, dest, []byte(interfacesJoin(entries))); err != nil {
			return Result{}, err
		}
	}

	result := Ok(dest)
	if changed {
		result = Changed(dest)
	}
	return result.WithExtra("dest", dest).WithExtra("ifaces", ifaces), nil
}

// ifLine is one raw line of an interfaces(5) file, tagged with what
// read_interfaces_lines-equivalent parsing determined it to be.
type ifLine struct {
	raw      string
	lineType string // "iface", "option", "other"
	iface    string // owning iface name, for both "iface" and "option" lines
	family   string // address_family, for both "iface" and "option" lines
	method   string // only meaningful for "iface" lines
	option   string // only meaningful for "option" lines
	value    string // only meaningful for "option" lines
}

var interfacesRepeatable = map[string]bool{"pre-up": true, "up": true, "down": true, "post-up": true}

// interfacesParseLines mirrors real interfaces_file's own
// read_interfaces_lines exactly, including that a blank line or
// comment does NOT end an open iface stanza.
func interfacesParseLines(content string) []ifLine {
	var entries []ifLine
	var currentIface, currentFamily string
	processingIface := false

	for _, raw := range splitLines(content) {
		fields := strings.Fields(raw)
		if len(fields) == 0 || fields[0][0] == '#' {
			entries = append(entries, ifLine{raw: raw, lineType: "other"})
			continue
		}
		switch {
		case fields[0] == "iface" && len(fields) >= 2:
			name := fields[1]
			family := ""
			if len(fields) > 2 {
				family = fields[2]
			}
			method := ""
			if len(fields) > 3 {
				method = fields[3]
			}
			currentIface, currentFamily = name, family
			entries = append(entries, ifLine{raw: raw, lineType: "iface", iface: name, family: family, method: method})
			processingIface = true
		case fields[0] == "mapping", fields[0] == "source", fields[0] == "source-dir",
			fields[0] == "source-directory", fields[0] == "auto", fields[0] == "no-auto-down",
			fields[0] == "no-scripts", strings.HasPrefix(fields[0], "allow-"):
			entries = append(entries, ifLine{raw: raw, lineType: "other"})
			processingIface = false
		default:
			if processingIface {
				option := fields[0]
				idx := strings.Index(raw, option)
				value := strings.TrimSpace(raw[idx+len(option):])
				entries = append(entries, ifLine{
					raw: raw, lineType: "option", iface: currentIface, family: currentFamily,
					option: option, value: value,
				})
			} else {
				entries = append(entries, ifLine{raw: raw, lineType: "other"})
			}
		}
	}
	return entries
}

// interfacesBuildFacts derives the `ifaces` return value from entries,
// matching real interfaces_file's own currif accumulation (including
// its "last stanza wins" quirk for a repeated iface name — see
// moduleInterfacesFile's own doc comment).
func interfacesBuildFacts(entries []ifLine) map[string]any {
	ifaces := map[string]any{}
	var currif map[string]any
	for _, e := range entries {
		switch e.lineType {
		case "iface":
			currif = map[string]any{
				"address_family": e.family,
				"method":         e.method,
				"pre-up":         []string{},
				"up":             []string{},
				"down":           []string{},
				"post-up":        []string{},
			}
			ifaces[e.iface] = currif
		case "option":
			if currif == nil {
				continue
			}
			if interfacesRepeatable[e.option] {
				currif[e.option] = append(currif[e.option].([]string), e.value)
			} else {
				currif[e.option] = e.value
			}
		}
	}
	return ifaces
}

// interfacesSetOption applies one option add/update/remove, returning
// the (possibly new) entries slice. A non-nil *Result means real
// interfaces_file would module.fail_json here (interface not found, or
// an unsupported state).
func interfacesSetOption(entries []ifLine, iface, option, value, state, addressFamily string) (bool, []ifLine, *Result) {
	var relevant []int
	for i, e := range entries {
		if (e.lineType == "iface" || e.lineType == "option") && e.iface == iface {
			if addressFamily != "" && e.family != addressFamily {
				continue
			}
			relevant = append(relevant, i)
		}
	}
	if len(relevant) == 0 {
		r := Fail("interfaces_file: interface " + iface + " not found")
		return false, entries, &r
	}

	var optionIdxs, targetIdxs []int
	for _, i := range relevant {
		if entries[i].lineType != "option" {
			continue
		}
		optionIdxs = append(optionIdxs, i)
		if entries[i].option == option {
			targetIdxs = append(targetIdxs, i)
		}
	}

	changed := false
	switch state {
	case "present":
		switch {
		case len(targetIdxs) == 0 && option == "method":
			for _, i := range relevant {
				if entries[i].lineType != "iface" || entries[i].method == value {
					continue
				}
				changed = true
				entries[i].raw = interfacesReplaceMethodSuffix(entries[i].raw, entries[i].method, value)
				entries[i].method = value
			}
		case len(targetIdxs) == 0:
			anchor := relevant[len(relevant)-1]
			entries = interfacesAddOption(entries, option, value, iface, addressFamily, anchor, len(optionIdxs) == 0)
			changed = true
		case interfacesRepeatable[option]:
			exists := false
			for _, i := range targetIdxs {
				if entries[i].value == value {
					exists = true
					break
				}
			}
			if !exists {
				anchor := targetIdxs[len(targetIdxs)-1]
				entries = interfacesAddOption(entries, option, value, iface, addressFamily, anchor, false)
				changed = true
			}
		default:
			last := targetIdxs[len(targetIdxs)-1]
			if entries[last].value != value {
				changed = true
				entries[last].raw = interfacesUpdateValue(entries[last].raw, option, entries[last].value, value)
				entries[last].value = value
			}
		}

	case "absent":
		if len(targetIdxs) > 0 {
			remove := map[int]bool{}
			if interfacesRepeatable[option] && value != "" {
				for _, i := range targetIdxs {
					if entries[i].value == value {
						remove[i] = true
					}
				}
			} else {
				for _, i := range targetIdxs {
					remove[i] = true
				}
			}
			if len(remove) > 0 {
				changed = true
				entries = interfacesRemoveIndices(entries, remove)
			}
		}

	default:
		r := Fail("interfaces_file: unsupported state " + state + ", has to be either present or absent")
		return false, entries, &r
	}

	return changed, entries, nil
}

// interfacesAddOption inserts a new option line right after
// entries[anchor], indented like anchor's own line (with an extra
// 4-space indent when noExistingOptions — anchor is the bare `iface`
// line itself, matching real addOptionAfterLine).
func interfacesAddOption(entries []ifLine, option, value, iface, addressFamily string, anchor int, noExistingOptions bool) []ifLine {
	prefix, suffix := interfacesPrefixSuffix(entries[anchor].raw)
	if noExistingOptions {
		prefix += "    "
	}
	newEntry := ifLine{
		raw: prefix + option + " " + value + suffix, lineType: "option",
		iface: iface, family: addressFamily, option: option, value: value,
	}
	out := make([]ifLine, 0, len(entries)+1)
	out = append(out, entries[:anchor+1]...)
	out = append(out, newEntry)
	out = append(out, entries[anchor+1:]...)
	return out
}

// interfacesPrefixSuffix splits line into the whitespace/text before
// its first word and anything after its last word — matching real
// addOptionAfterLine's own `prefix`/line[suffix_start:]` slicing.
func interfacesPrefixSuffix(line string) (prefix, suffix string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", ""
	}
	first, last := fields[0], fields[len(fields)-1]
	prefixEnd := strings.Index(line, first)
	suffixStart := strings.LastIndex(line, last) + len(last)
	return line[:prefixEnd], line[suffixStart:]
}

// interfacesUpdateValue replaces oldValue (found as a literal substring
// after option's own first occurrence in raw — guaranteed to be found,
// since oldValue was itself extracted from this same raw line) with
// newValue, preserving everything else on the line (indentation, any
// trailing inline comment).
func interfacesUpdateValue(raw, option, oldValue, newValue string) string {
	optionStart := strings.Index(raw, option)
	afterOption := raw[optionStart+len(option):]
	idx := strings.Index(afterOption, oldValue)
	if idx < 0 {
		return raw[:optionStart+len(option)] + " " + newValue
	}
	valueStart := optionStart + len(option) + idx
	valueEnd := valueStart + len(oldValue)
	return raw[:valueStart] + newValue + raw[valueEnd:]
}

// interfacesReplaceMethodSuffix rewrites an `iface` line's own trailing
// method token, matching real addOptionAfterLine's own
// `re.sub(f"{old_method}$", value, line)`.
func interfacesReplaceMethodSuffix(raw, oldMethod, newMethod string) string {
	if oldMethod == "" {
		return raw + newMethod
	}
	if strings.HasSuffix(raw, oldMethod) {
		return raw[:len(raw)-len(oldMethod)] + newMethod
	}
	return raw
}

func interfacesRemoveIndices(entries []ifLine, remove map[int]bool) []ifLine {
	out := make([]ifLine, 0, len(entries))
	for i, e := range entries {
		if remove[i] {
			continue
		}
		out = append(out, e)
	}
	return out
}

func interfacesJoin(entries []ifLine) string {
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = e.raw
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	return content
}
