package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXfconf implements Ansible's `xfconf` (community.general) module:
// sets or resets an XFCE 4 configuration property via the `xfconf-query`
// CLI tool — the same binary real xfconf's own module wraps (there is
// no library form to substitute: real xfconf already shells out to
// `xfconf-query` itself, so this port's substitution is the binary
// itself, not an architectural stand-in like consul_kv.go's own CLI
// substitution for an HTTP API).
//
// Args: channel (required); property (required); value ([]raw,
// required for state=present) — each entry is stringified the same way
// this package's own argString/fmt.Sprint does elsewhere (dconf.go's
// own doc comment notes fmt.Sprint(true)/(false) already match xfconf's
// own expected "true"/"false" spelling); value_type ([]string:
// string|int|double|bool|uint|uchar|char|uint64|int64|float) — if
// omitted, this port infers one per value from its own Go type (bool->
// bool, int/int64->int, float64->double, else->string), a simplification
// vs real xfconf's own more exact Python-type-driven inference; if a
// single value_type is given for multiple values, matching real xfconf,
// it applies to all of them. force_array (alias array, bool, default
// false) — `--force-array`, needed to set a genuine one-element array
// rather than a scalar. state (present|absent, default present) —
// absent runs `xfconf-query --reset` (real xfconf's own documented
// behavior: this only removes a user-configured override, reverting to
// any system default, never truly deleting the property).
//
// Idempotency: this port always reads the property first (`xfconf-query
// --channel --property`, with no --create/--set) and compares against
// the desired value(s) before writing — for an array, `xfconf-query`
// prints "Value is an array with N items:\n\n" followed by one value
// per line (confirmed against xfconf's own upstream main.c source,
// since ansible-doc's own EXAMPLES/RETURN VALUES do not show this raw
// CLI output shape); a scalar read is just the value on its own line.
//
// Extra: channel, property, cmd ([]string, the xfconf-query argv used —
// this port's own version always starts with the bare "xfconf-query"
// rather than real xfconf's own resolved absolute binary path, since
// this port never resolves one), previous_value, value, value_type
// (each a single string when scalar, a []string when array — matching
// real xfconf's own documented "string or a list of strings" RETURN
// VALUES shape), version (`xfconf-query --version`'s own first line,
// last whitespace-separated token).
func moduleXfconf(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	channel, err := requireString(args, "channel")
	if err != nil {
		return Result{}, err
	}
	property, err := requireString(args, "property")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("xfconf: state must be present or absent, got %q", state)
	}

	version, _ := xfconfVersion(ctx, conn)

	curIsArray, curScalar, curValues, curExists, err := xfconfRead(ctx, conn, channel, property)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !curExists {
			return Ok("").WithExtra("channel", channel).WithExtra("property", property).WithExtra("version", version), nil
		}
		res, err := runStatus(ctx, conn, "xfconf-query --channel "+shellQuote(channel)+" --property "+shellQuote(property)+" --reset")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("xfconf: reset failed: " + strings.TrimSpace(res.Stderr)), nil
		}
		r := Changed("").WithExtra("channel", channel).WithExtra("property", property).WithExtra("version", version)
		r = r.WithExtra("value_type", "none")
		if curIsArray {
			r = r.WithExtra("previous_value", curValues)
		} else {
			r = r.WithExtra("previous_value", curScalar)
		}
		return r, nil
	}

	rawValues, ok := args["value"].([]any)
	if !ok || len(rawValues) == 0 {
		return Result{}, errArg("xfconf: value is required when state is present")
	}
	types := argStringList(args, "value_type")
	strs := make([]string, len(rawValues))
	valueTypes := make([]string, len(rawValues))
	for i, v := range rawValues {
		strs[i] = fmt.Sprint(v)
		switch {
		case len(types) == len(rawValues):
			valueTypes[i] = types[i]
		case len(types) == 1:
			valueTypes[i] = types[0]
		default:
			valueTypes[i] = xfconfInferType(v)
		}
	}
	forceArray := argBool(args, "force_array", argBool(args, "array", false))
	isArray := forceArray || len(strs) > 1

	unchanged := false
	if isArray {
		unchanged = curExists && curIsArray && consulACLStrSliceEqual(curValues, strs)
	} else {
		unchanged = curExists && !curIsArray && curScalar == strs[0]
	}
	if unchanged {
		r := Ok("").WithExtra("channel", channel).WithExtra("property", property).WithExtra("version", version)
		return xfconfExtraValues(r, strs, valueTypes, isArray), nil
	}

	cmdArgs := []string{"xfconf-query", "--channel", channel, "--property", property, "--create"}
	for i, v := range strs {
		cmdArgs = append(cmdArgs, "--type", valueTypes[i], "--set", v)
	}
	if forceArray {
		cmdArgs = append(cmdArgs, "--force-array")
	}
	quoted := make([]string, len(cmdArgs))
	for i, a := range cmdArgs {
		quoted[i] = shellQuote(a)
	}
	res, err := conn.Exec(ctx, strings.Join(quoted, " "), nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("xfconf: unable to set " + channel + " " + property + ": " + strings.TrimSpace(res.Stderr)), nil
	}

	r := Changed("").WithExtra("channel", channel).WithExtra("property", property).
		WithExtra("cmd", cmdArgs).WithExtra("version", version)
	if curIsArray {
		r = r.WithExtra("previous_value", curValues)
	} else if curExists {
		r = r.WithExtra("previous_value", curScalar)
	}
	return xfconfExtraValues(r, strs, valueTypes, isArray), nil
}

func xfconfExtraValues(r Result, strs, types []string, isArray bool) Result {
	if isArray {
		return r.WithExtra("value", strs).WithExtra("value_type", types)
	}
	return r.WithExtra("value", strs[0]).WithExtra("value_type", types[0])
}

// xfconfInferType guesses a value_type from v's own Go type when the
// module argument omits value_type — see this module's own doc comment
// for the simplification this represents vs real xfconf's own more
// exact Python-type-driven inference.
func xfconfInferType(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case int, int64:
		return "int"
	case float64, float32:
		return "double"
	default:
		return "string"
	}
}

// xfconfVersion runs `xfconf-query --version` and returns the last
// whitespace-separated token of its first line (e.g. "4.18.1" from
// "xfconf-query 4.18.1"). A non-zero exit or unparseable output yields
// an empty string rather than an error, since the version is a
// best-effort informational Extra field, not something either module
// depends on for its own logic.
func xfconfVersion(ctx context.Context, conn remoteexec.Connection) (string, error) {
	res, err := runStatus(ctx, conn, "xfconf-query --version")
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", nil
	}
	first, _, _ := strings.Cut(res.Stdout, "\n")
	fields := strings.Fields(first)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[len(fields)-1], nil
}

// xfconfRead runs `xfconf-query --channel C --property P` (no
// --create/--set: a plain read) and parses its output — see this
// module's own doc comment for the "Value is an array with N items:"
// array format, confirmed against xfconf's own upstream source. exists
// is false (not an error) for a non-zero exit, matching this port's
// other read-then-decide modules (e.g. consul_kv.go's own
// consulKvExistingValue) treating "property not set" as an expected
// outcome.
func xfconfRead(ctx context.Context, conn remoteexec.Connection, channel, property string) (isArray bool, scalar string, values []string, exists bool, err error) {
	res, err := runStatus(ctx, conn, "xfconf-query --channel "+shellQuote(channel)+" --property "+shellQuote(property))
	if err != nil {
		return false, "", nil, false, err
	}
	if res.RC != 0 {
		return false, "", nil, false, nil
	}
	out := strings.TrimRight(res.Stdout, "\n")
	if strings.HasPrefix(out, "Value is an array with") {
		_, rest, found := strings.Cut(out, "\n\n")
		if !found {
			return true, "", nil, true, nil
		}
		for _, line := range strings.Split(rest, "\n") {
			values = append(values, line)
		}
		return true, "", values, true, nil
	}
	return false, out, nil, true, nil
}
