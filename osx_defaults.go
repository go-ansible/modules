package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOsxDefaults implements (a subset of) Ansible's `osx_defaults`
// (community.general) module: reads, writes, or deletes a macOS user
// preference via the `defaults` CLI tool — read from real
// osx_defaults.py's own OSXDefaults class (this batch's hard rule:
// several of the type-conversion/comparison rules below, and the
// `dict`-type plutil round-trip, are only visible in the
// implementation).
//
// Args: domain (string, default "NSGlobalDomain"); host (string) —
// "currentHost" maps to `-currentHost`, anything else to `-host
// <host>`; key (string); type (array|bool|boolean|date|dict|float|
// int|integer|string, default "string"); value (raw, required unless
// state=absent); state (present|absent|list, default "present");
// array_add (bool, default false) — for type=array, only writes
// elements not already present (via `-array-add`); check_type (bool,
// default true) — fails if the CURRENT default's own type doesn't
// match the given type; dict_mode (replace|add, default "replace") —
// for type=dict, "add" merges given keys into the existing dict rather
// than replacing it wholesale.
//
// This port hard-requires `defaults` on PATH (a `command -v defaults`
// gate, matching real osx_defaults' own `get_bin_path(required=False)`
// + explicit "Unable to locate defaults executable" failure) and, for
// type=dict, additionally requires `plutil` (real osx_defaults' own
// dict-read path shells out to `plutil -extract ... json` for
// type-preserving JSON conversion, since `defaults read`'s own
// old-style plist text output loses boolean-vs-1/0 type information).
// It does NOT search the `path` argument's own directory list the way
// real osx_defaults does (`get_bin_path(opt_dirs=path.split(":"))`) —
// this port always resolves `defaults`/`plutil` via the target shell's
// own PATH, ignoring the `path` argument entirely; a target with
// `defaults` outside its login PATH but reachable via the documented
// default `/usr/bin:/usr/local/bin` would need that PATH entry added
// some other way.
//
// Type conversion mirrors real osx_defaults' own _convert_type: bool/
// boolean accepts true/false/1/0/"true"/"false"/"1"/"0"/"yes"/"no"
// (case-insensitive for strings); int/integer requires an all-digit
// (optionally "-"-prefixed) string/number; array requires a list;
// dict requires a map. "date" is NOT implemented by this port (real
// osx_defaults parses "yyyy-mm-dd hh:mm:ss" into a Python datetime and
// writes it back via `-date`; this port has no matching write path —
// state=present with type=date returns Result{Failed:true} rather than
// silently mishandling the value).
//
// Reading: `defaults read-type <domain> <key>` first (RC 1 means unset
// -> current value nil, matching real osx_defaults' own read()); then
// `defaults read <domain> <key>` for the value itself. type=array
// output is parsed by stripping the surrounding "(\n...\n)" wrapper
// real `defaults read`'s own array text format always produces (one
// quoted, comma-trailing element per line) — same rule as real
// osx_defaults' own _convert_defaults_str_to_list. type=dictionary
// output is read via `defaults export <domain> <tmpfile>` followed by
// `plutil -extract <key> json -o - <tmpfile>`, exactly mirroring real
// osx_defaults' own dict-read path (and its own documented reason: a
// plain `defaults read` on a dict value loses boolean type fidelity).
//
// Writing: type=dict passes each key as its own `-<type> <value>`
// token pair (bool -> -bool TRUE/FALSE, int -> -int, float -> -float,
// anything else -> -string), using `-dict` (replace) or `-dict-add`
// (dict_mode=add and a current value already exists) as real
// osx_defaults' own write() does. Non-dict types write a single value
// (bool -> TRUE/FALSE, array_add computes the set difference against
// the current array first, matching real osx_defaults' own
// `list(set(value) - set(current_value))`).
//
// Idempotency mirrors real osx_defaults' own run(): array (without
// array_add) compares as SETS (order-insensitive, duplicates
// collapsed) against the current value; array with array_add is
// unchanged when the given elements are already all present; dict
// with dict_mode=add is unchanged when every given key already has
// the given value in the current dict (extra existing keys are
// ignored, matching dict_mode=add's own "leave other keys untouched"
// semantics); everything else compares by exact equality.
//
// Simplifications vs real osx_defaults: no `path` argument support
// (see above); type=date is not implemented (see above); this port
// does not localize command output (no LANGUAGE=C/LC_ALL=C override,
// since remoteexec.Connection's Exec has no per-call environment
// parameter) — a cosmetic difference only.
func moduleOsxDefaults(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	domain := argString(args, "domain", "NSGlobalDomain")
	host := argString(args, "host", "")
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	typ := argString(args, "type", "string")
	switch typ {
	case "array", "bool", "boolean", "date", "dict", "float", "int", "integer", "string":
	default:
		return Result{}, errArg("osx_defaults: type must be one of array, bool, boolean, date, dict, float, int, integer, string, got %q", typ)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "list" {
		return Result{}, errArg("osx_defaults: state must be present, absent, or list, got %q", state)
	}
	checkType := argBool(args, "check_type", true)
	arrayAdd := argBool(args, "array_add", false)
	dictMode := argString(args, "dict_mode", "replace")
	if dictMode != "replace" && dictMode != "add" {
		return Result{}, errArg("osx_defaults: dict_mode must be replace or add, got %q", dictMode)
	}

	if _, err := run(ctx, conn, "command -v defaults"); err != nil {
		return Fail("osx_defaults: defaults executable not found on the target"), nil
	}
	if typ == "dict" {
		if _, err := run(ctx, conn, "command -v plutil"); err != nil {
			return Fail("osx_defaults: plutil executable not found on the target (required for type=dict)"), nil
		}
	}

	base := "defaults"
	switch host {
	case "":
	case "currentHost":
		base += " -currentHost"
	default:
		base += " -host " + shellQuote(host)
	}

	currentType, currentRaw, err := osxDefaultsRead(ctx, conn, base, domain, key)
	if err != nil {
		return Result{}, err
	}

	if state == "list" {
		var val any
		if currentType != "" {
			v, err := osxDefaultsParsedValue(ctx, conn, base, domain, key, currentType, currentRaw)
			if err != nil {
				return Result{}, err
			}
			val = v
		}
		return Ok("").WithExtra("key", key).WithExtra("value", val), nil
	}

	if state == "absent" {
		if currentType == "" {
			return Ok(""), nil
		}
		if _, err := run(ctx, conn, base+" delete "+shellQuote(domain)+" "+shellQuote(key)); err != nil {
			return Result{}, err
		}
		return Changed(""), nil
	}

	// state == "present"
	if _, ok := args["value"]; !ok {
		return Result{}, errArg("osx_defaults: value is required when state is present")
	}
	if typ == "date" {
		return Fail("osx_defaults: type=date is not supported by this port (no target-side date write path — see moduleOsxDefaults' own doc comment)"), nil
	}

	var currentValue any
	if currentType != "" {
		v, err := osxDefaultsParsedValue(ctx, conn, base, domain, key, currentType, currentRaw)
		if err != nil {
			return Result{}, err
		}
		currentValue = v
	}

	if checkType && currentValue != nil {
		if mismatch := osxDefaultsTypeMismatch(typ, currentValue); mismatch != "" {
			return Fail("osx_defaults: type mismatch. Type in defaults: " + mismatch), nil
		}
	}

	value, ferr := osxDefaultsConvert(typ, args["value"])
	if ferr != "" {
		return Fail("osx_defaults: " + ferr), nil
	}

	if osxDefaultsUnchanged(typ, arrayAdd, dictMode, currentValue, value) {
		return Ok(""), nil
	}

	if err := osxDefaultsWrite(ctx, conn, base, domain, key, typ, value, arrayAdd, dictMode, currentValue != nil); err != nil {
		return Result{}, err
	}
	return Changed(""), nil
}

// osxDefaultsRead runs `defaults read-type` to determine whether key is
// set at all (and its real `defaults`-reported type string, e.g.
// "array", "boolean", "dictionary"), matching real OSXDefaults.read's
// own two-step probe. Returns ("", "", nil) if unset (RC 1).
func osxDefaultsRead(ctx context.Context, conn remoteexec.Connection, base, domain, key string) (typ, raw string, err error) {
	res, err := runStatus(ctx, conn, base+" read-type "+shellQuote(domain)+" "+shellQuote(key))
	if err != nil {
		return "", "", err
	}
	if res.RC == 1 {
		return "", "", nil
	}
	if res.RC != 0 {
		return "", "", fmt.Errorf("osx_defaults: reading key type: %s", strings.TrimSpace(res.Stderr))
	}
	typ = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(res.Stdout), "Type is "))

	res, err = runStatus(ctx, conn, base+" read "+shellQuote(domain)+" "+shellQuote(key))
	if err != nil {
		return "", "", err
	}
	if res.RC != 0 {
		return "", "", fmt.Errorf("osx_defaults: reading key value: %s", strings.TrimSpace(res.Stderr))
	}
	return typ, strings.TrimSpace(res.Stdout), nil
}

// osxDefaultsParsedValue converts raw `defaults read` output (or, for
// a dictionary, a plutil-extracted JSON value) per its real `defaults`-
// reported type, into a Go value comparable against a freshly-converted
// argument value (string, bool, int64, float64, []string, or
// map[string]any).
func osxDefaultsParsedValue(ctx context.Context, conn remoteexec.Connection, base, domain, key, currentType, raw string) (any, error) {
	switch currentType {
	case "array":
		return osxDefaultsParseArray(raw), nil
	case "boolean":
		return raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes"), nil
	case "integer":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return raw, nil
		}
		return n, nil
	case "float":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return raw, nil
		}
		return f, nil
	case "dictionary":
		return osxDefaultsReadDict(ctx, conn, base, domain, key)
	default:
		return raw, nil
	}
}

// osxDefaultsParseArray mirrors real OSXDefaults._convert_defaults_str_to_list:
// strips the leading "(" and trailing ")" lines of `defaults read`'s
// own array text format, then trims each element's surrounding quotes/
// comma/whitespace and unescapes `\"`.
var osxDefaultsArrayElemRe = regexp.MustCompile(`^ *"?|"?,? *$`)

func osxDefaultsParseArray(raw string) []string {
	lines := strings.Split(raw, "\n")
	if len(lines) >= 2 {
		lines = lines[1 : len(lines)-1]
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.ReplaceAll(l, `\"`, `"`)
		l = osxDefaultsArrayElemRe.ReplaceAllString(l, "")
		out = append(out, l)
	}
	return out
}

// osxDefaultsReadDict mirrors real OSXDefaults.read's own dictionary
// path: export the domain to a target-side temp plist file, then
// `plutil -extract <key> json -o -` it for type-preserving JSON.
func osxDefaultsReadDict(ctx context.Context, conn remoteexec.Connection, base, domain, key string) (map[string]any, error) {
	tmp := conn.TempPath("osx_defaults.plist")
	defer func() { _ = conn.Remove(ctx, tmp) }()
	if _, err := run(ctx, conn, base+" export "+shellQuote(domain)+" "+shellQuote(tmp)); err != nil {
		return nil, err
	}
	out, err := run(ctx, conn, "plutil -extract "+shellQuote(key)+" json -o - "+shellQuote(tmp))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if jerr := json.Unmarshal([]byte(out), &m); jerr != nil {
		return nil, fmt.Errorf("osx_defaults: parsing plutil JSON output: %w", jerr)
	}
	return m, nil
}

// osxDefaultsTypeMismatch reports real OSXDefaults.run's own
// check_type failure text (the Python type name of the CURRENT value)
// if typ's Go representation doesn't match currentValue's own Go type,
// or "" if they match.
func osxDefaultsTypeMismatch(typ string, currentValue any) string {
	wantsBool := typ == "bool" || typ == "boolean"
	wantsInt := typ == "int" || typ == "integer"
	wantsFloat := typ == "float"
	wantsArray := typ == "array"
	wantsDict := typ == "dict"
	wantsString := typ == "string"

	switch currentValue.(type) {
	case bool:
		if !wantsBool {
			return "bool"
		}
	case int64:
		if !wantsInt {
			return "int"
		}
	case float64:
		if !wantsFloat {
			return "float"
		}
	case []string:
		if !wantsArray {
			return "list"
		}
	case map[string]any:
		if !wantsDict {
			return "dict"
		}
	case string:
		if !wantsString {
			return "str"
		}
	}
	return ""
}

// osxDefaultsConvert mirrors real OSXDefaults._convert_type, producing
// the same Go value shapes osxDefaultsParsedValue produces for reading
// (string, bool, int64, float64, []string, or map[string]any), so the
// two sides compare cleanly. ferr is real _convert_type's own
// OSXDefaultsException text, non-empty on an invalid value for typ.
func osxDefaultsConvert(typ string, value any) (out any, ferr string) {
	switch typ {
	case "string":
		return fmt.Sprint(value), ""
	case "bool", "boolean":
		switch v := value.(type) {
		case bool:
			return v, ""
		case string:
			switch strings.ToLower(v) {
			case "true", "1", "yes":
				return true, ""
			case "false", "0", "no":
				return false, ""
			}
		case int:
			if v == 1 {
				return true, ""
			}
			if v == 0 {
				return false, ""
			}
		case float64:
			if v == 1 {
				return true, ""
			}
			if v == 0 {
				return false, ""
			}
		}
		return nil, fmt.Sprintf("invalid boolean value: %v", value)
	case "int", "integer":
		s := fmt.Sprint(value)
		neg := strings.HasPrefix(s, "-")
		digits := s
		if neg {
			digits = s[1:]
		}
		if digits == "" || !isAllDigits(digits) {
			return nil, fmt.Sprintf("invalid integer value: %v", value)
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Sprintf("invalid integer value: %v", value)
		}
		return n, ""
	case "float":
		switch v := value.(type) {
		case float64:
			return v, ""
		case int:
			return float64(v), ""
		case string:
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil, fmt.Sprintf("invalid float value: %v", value)
			}
			return f, ""
		}
		return nil, fmt.Sprintf("invalid float value: %v", value)
	case "array":
		list := argAnyList(value)
		if list == nil {
			return nil, "invalid value. Expected value to be an array"
		}
		out := make([]string, len(list))
		for i, v := range list {
			out[i] = fmt.Sprint(v)
		}
		return out, ""
	case "dict", "dictionary":
		m, ok := value.(map[string]any)
		if !ok {
			return nil, "invalid value. Expected value to be a dict"
		}
		return m, ""
	}
	return nil, "type is not supported: " + typ
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// argAnyList returns v as a []any if it's a list-shaped value (either
// []any from JSON/YAML decoding, or a native Go slice via reflection-
// free type switches for the common cases this port's callers produce),
// or nil otherwise.
func argAnyList(v any) []any {
	switch l := v.(type) {
	case []any:
		return l
	case []string:
		out := make([]any, len(l))
		for i, s := range l {
			out[i] = s
		}
		return out
	}
	return nil
}

// osxDefaultsUnchanged mirrors real OSXDefaults.run's own idempotency
// checks (see moduleOsxDefaults' own doc comment for the per-type
// rules).
func osxDefaultsUnchanged(typ string, arrayAdd bool, dictMode string, currentValue, value any) bool {
	if typ == "array" {
		curArr, curOK := currentValue.([]string)
		newArr, _ := value.([]string)
		if curOK {
			if !arrayAdd {
				return osxDefaultsSetEqual(curArr, newArr)
			}
			return len(osxDefaultsSetDiff(newArr, curArr)) == 0
		}
	}
	if typ == "dict" && dictMode == "add" {
		curMap, curOK := currentValue.(map[string]any)
		newMap, _ := value.(map[string]any)
		if curOK {
			for k, v := range newMap {
				if !osxDefaultsJSONEqual(curMap[k], v) {
					return false
				}
			}
			return true
		}
	}
	return osxDefaultsJSONEqual(currentValue, value)
}

// osxDefaultsJSONEqual compares two of this file's own value shapes
// (string/bool/int64/float64/[]string/map[string]any) for equality,
// treating a nil currentValue (key unset) as never equal — matching
// real OSXDefaults.run's own `self.current_value == self.value` after
// read() leaves current_value as None for an unset key, which never
// equals any converted value.
func osxDefaultsJSONEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	switch av := a.(type) {
	case []string:
		bv, ok := b.([]string)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !osxDefaultsJSONEqual(v, bv[k]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// osxDefaultsSetEqual reports whether a and b hold the same DISTINCT
// elements, ignoring order AND duplicate counts — matching Python's
// `set(a) == set(b)`, which is what real osx_defaults' own array-type
// idempotency check literally uses. This is deliberately distinct from
// this file's own stringSetEqual below (shared with ipa_common.go,
// which preserves duplicate counts): a value list containing a genuine
// duplicate is a corner case neither this port nor real osx_defaults'
// own `defaults` CLI meaningfully round-trips anyway (osx_defaults'
// own comparison already collapses duplicates on both sides before
// comparing).
func osxDefaultsSetEqual(a, b []string) bool {
	setA := map[string]bool{}
	for _, s := range a {
		setA[s] = true
	}
	setB := map[string]bool{}
	for _, s := range b {
		setB[s] = true
	}
	if len(setA) != len(setB) {
		return false
	}
	for k := range setA {
		if !setB[k] {
			return false
		}
	}
	return true
}

// stringSetEqual reports whether a and b hold the same strings in the
// same MULTISET (order-insensitive, but duplicate counts must match) —
// shared with ipa_common.go's own use for comparing ipa_* module list
// arguments, where a real duplicate value is meaningful and shouldn't
// silently collapse the way osxDefaultsSetEqual's own dedup does.
func stringSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// osxDefaultsSetDiff returns the elements of a not present in b (a set
// difference), matching Python's `set(value) - set(current_value)`.
func osxDefaultsSetDiff(a, b []string) []string {
	inB := map[string]bool{}
	for _, s := range b {
		inB[s] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range a {
		if inB[s] || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// osxDefaultsWrite mirrors real OSXDefaults.write.
func osxDefaultsWrite(ctx context.Context, conn remoteexec.Connection, base, domain, key, typ string, value any, arrayAdd bool, dictMode string, hasCurrent bool) error {
	if typ == "dict" {
		m := value.(map[string]any)
		effectiveType := "dict"
		if dictMode == "add" && hasCurrent {
			effectiveType = "dict-add"
		}
		cmd := base + " write " + shellQuote(domain) + " " + shellQuote(key) + " -" + effectiveType
		for k, v := range m {
			cmd += " " + shellQuote(k) + osxDefaultsDictValueArgs(v)
		}
		_, err := run(ctx, conn, cmd)
		return err
	}

	writeType := typ
	var tokens []string
	switch v := value.(type) {
	case bool:
		if v {
			tokens = []string{"TRUE"}
		} else {
			tokens = []string{"FALSE"}
		}
	case int64:
		tokens = []string{strconv.FormatInt(v, 10)}
	case float64:
		tokens = []string{strconv.FormatFloat(v, 'f', -1, 64)}
	case []string:
		if typ == "array" && arrayAdd {
			writeType = "array-add"
		}
		tokens = v
	case string:
		tokens = []string{v}
	}

	cmd := base + " write " + shellQuote(domain) + " " + shellQuote(key) + " -" + writeType
	for _, t := range tokens {
		cmd += " " + shellQuote(t)
	}
	_, err := run(ctx, conn, cmd)
	return err
}

// osxDefaultsDictValueArgs mirrors real OSXDefaults._dict_value_to_args:
// " -type value" tokens appended after a dict entry's own key.
func osxDefaultsDictValueArgs(v any) string {
	switch val := v.(type) {
	case bool:
		if val {
			return " -bool TRUE"
		}
		return " -bool FALSE"
	case int64:
		return " -int " + strconv.FormatInt(val, 10)
	case int:
		return " -int " + strconv.Itoa(val)
	case float64:
		return " -float " + strconv.FormatFloat(val, 'f', -1, 64)
	default:
		return " -string " + shellQuote(fmt.Sprint(v))
	}
}
