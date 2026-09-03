package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGconftool2 implements (a subset of) Ansible's `gconftool2`
// (community.general) module: sets or unsets one GConf preference key
// via the `gconftool-2` CLI — read from real gconftool2.py's own
// GConftool StateModuleHelper class and module_utils/_gconftool2.py's
// own gconftool2_runner arg_formats (this batch's hard rule: the exact
// per-state argument list and the "direct/config_source apply to --set
// but NOT to --get/--unset" asymmetry are only visible in the
// implementation, not EXAMPLES/OPTIONS).
//
// Args: key (string, required); state (present|absent, required — no
// default, matching real gconftool2's own required=True); value
// (string) and value_type (bool|float|int|string), both required when
// state=present (matching real required_if); direct (bool, default
// false) — access the config database directly, bypassing gconfd;
// config_source (string, optional) — required together with direct
// (matching real required_if).
//
// A real, faithfully-reproduced asymmetry: `direct`/`config_source`
// are appended to the `--set` invocation for state=present, but are
// NEVER passed to the internal `--get` probe (used for both
// previous_value and, after a --set, new_value) or to the `--unset`
// invocation for state=absent — matching real GConftool's own
// _get()/state_absent() runner calls, which list only "state key" as
// their own argument order, omitting direct/config_source entirely
// (state_present's own runner call is the only one listing them). This
// is reproduced exactly, not "fixed" into a more consistent shape this
// port's own implementation never had a mandate to invent.
//
// A second real, faithfully-reproduced quirk: gconftool-2 is always
// invoked (--set for present, --unset for absent), with NO idempotency
// short-circuit of any kind — even when the key already holds the
// exact requested value, --set still runs. `changed` is computed
// separately, by comparing the key's value before and after (matching
// real GConftool's own StateModuleHelper diff-tracking of `_value`)
// rather than by skipping the command outright.
//
// A --get probe (used for both previous_value and post-`--set` value)
// treats a non-zero exit OR empty stdout as "no value set" (nil), never
// as a failure — this port could not verify gconftool-2's own exact
// exit-code behavior for a missing key against a real binary (not
// available in this port's build/test environment), so it follows
// real gconftool2's own documented RETURN VALUES text instead ("returns
// null for a non-existent key" as of community.general 7.0.0) as the
// most directly verifiable source of truth for that case, documented
// here as an assumption rather than a verified fact.
//
// `--set`'s own stderr is checked for state=present specifically (any
// non-empty stderr fails the module, matching real state_present's own
// output_process fail_on_err=True); state=absent's own stderr is
// ignored, matching real state_absent's own fail_on_err=False. A
// non-zero exit from EITHER --set or --unset is always a Fail.
//
// Returns Extra["key"], Extra["previous_value"], Extra["value"] (nil
// for state=absent), Extra["value_type"] (state=present only), and
// Extra["version"] (`gconftool-2 --version`'s trimmed output, always
// populated, matching real RETURN's own always-returned `version`).
func moduleGconftool2(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("gconftool2: state must be present or absent, got %q", state)
	}
	direct := argBool(args, "direct", false)
	configSource := argString(args, "config_source", "")
	if direct && configSource == "" {
		return Result{}, errArg("gconftool2: config_source is required when direct is true")
	}

	var value, valueType string
	if state == "present" {
		value, err = requireString(args, "value")
		if err != nil {
			return Result{}, errArg("gconftool2: value is required when state is present")
		}
		valueType, err = requireString(args, "value_type")
		if err != nil {
			return Result{}, errArg("gconftool2: value_type is required when state is present")
		}
	}

	if _, err := run(ctx, conn, "command -v gconftool-2"); err != nil {
		return Fail("gconftool2: gconftool-2 executable not found on the target"), nil
	}
	version, err := run(ctx, conn, "gconftool-2 --version")
	if err != nil {
		return Result{}, err
	}

	previous, err := gconftool2Get(ctx, conn, key)
	if err != nil {
		return Result{}, err
	}

	result := Ok("").WithExtra("key", key).WithExtra("version", version).WithExtra("previous_value", gconftool2Any(previous))

	if state == "absent" {
		res, err := runStatus(ctx, conn, "gconftool-2 --unset "+shellQuote(key))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("gconftool2: gconftool-2 failed with error:\n" + strings.TrimSpace(res.Stderr)), nil
		}
		result = result.WithExtra("value", nil)
		if previous != nil {
			result.Changed = true
		}
		return result, nil
	}

	cmd := "gconftool-2"
	if direct {
		cmd += " --direct"
	}
	if configSource != "" {
		cmd += " --config-source " + shellQuote(configSource)
	}
	cmd += " --type " + shellQuote(valueType) + " --set " + shellQuote(key) + " " + shellQuote(value)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 || strings.TrimSpace(res.Stderr) != "" {
		return Fail("gconftool2: gconftool-2 failed with error:\n" + strings.TrimSpace(res.Stderr)), nil
	}

	newValue, err := gconftool2Get(ctx, conn, key)
	if err != nil {
		return Result{}, err
	}
	result = result.WithExtra("value", gconftool2Any(newValue)).WithExtra("value_type", valueType)
	if !gconftool2Equal(previous, newValue) {
		result.Changed = true
	}
	return result, nil
}

// gconftool2Get runs `gconftool-2 --get <key>` and returns its value,
// or nil if unset — see moduleGconftool2's own doc comment for why a
// non-zero exit is treated the same as "no value" here rather than as
// a failure.
func gconftool2Get(ctx context.Context, conn remoteexec.Connection, key string) (*string, error) {
	res, err := runStatus(ctx, conn, "gconftool-2 --get "+shellQuote(key))
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	out := strings.TrimRight(res.Stdout, "\n")
	if out == "" {
		return nil, nil
	}
	return &out, nil
}

func gconftool2Any(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func gconftool2Equal(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
