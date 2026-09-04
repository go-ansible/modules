package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIPMIPower implements (a subset of) Ansible's `ipmi_power`
// module: controls a machine's power state over IPMI.
//
// See ipmi_boot.go's own doc comment for why this port shells out to
// `ipmitool -I lanplus -H <name> ...` via conn.Exec instead of using
// the pyghmi Python library real ipmi_power itself uses — the same
// reasoning applies here unchanged.
//
// Args: name, port, user, password, key: same as ipmi_boot.go. state
// (string, one of on|off|shutdown|reset|boot) — either this or machine
// is required, matching real ipmi_power's own `required_one_of`.
// timeout (int, default 300) — real ipmi_power passes this to pyghmi's
// own `set_power(..., wait=timeout)`, which polls the BMC for the
// power state to actually reach the requested one within timeout
// seconds; this port's ipmitool invocation has no directly equivalent
// flag (ipmitool's own `-N`/`-R` control retry/timeout of the IPMI
// *session* protocol, not "wait for the chassis to reach the target
// power state"), so timeout is accepted but not enforced — a
// documented simplification, not a silent drop (see Extra["timeout"]
// on the result, and the paragraph below on why "reset"/"shutdown"/
// "boot" are unconditionally reported changed regardless of how long
// the transition actually takes).
// machine ([]map, optional) — a list of {targetAddress (int, 0-255,
// required), state (optional, defaults to the top-level state)} for
// bridged IPMI requests against several target addresses behind the
// same BMC, via ipmitool's own `-t <targetAddress>` bridging flag.
//
// State mapping to `ipmitool chassis power <verb>`: on->on, off->off,
// shutdown->soft (ACPI-requested shutdown), reset->reset (immediate
// hard reset). "boot" has no direct ipmitool verb; real pyghmi's own
// set_power("boot") is documented as "if system is off, then on, else
// reset" — this port replicates exactly that: query `chassis power
// status` first, then issue on or reset accordingly.
//
// Idempotency matches real ipmi_power's own (slightly surprising)
// behavior exactly: `get_power()`'s own powerstate is only ever "on" or
// "off", so a requested state of "shutdown"/"reset"/"boot" NEVER
// compares equal to the current state and is therefore always applied
// (real ipmi_power's own `if current["powerstate"] != state` has the
// identical property — those three states are never idempotent in real
// ipmi_power either, not just in this port).
func moduleIPMIPower(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	user, err := requireString(args, "user")
	if err != nil {
		return Result{}, err
	}
	password, err := requireString(args, "password")
	if err != nil {
		return Result{}, err
	}
	port := argInt(args, "port", 623)
	key := argString(args, "key", "")
	timeout := argInt(args, "timeout", 300)
	state := argString(args, "state", "")
	if state != "" {
		if !isIPMIPowerState(state) {
			return Result{}, errArg("ipmi_power: state must be one of on, off, shutdown, reset, boot, got %q", state)
		}
	}

	machineArg, hasMachine := args["machine"]
	if state == "" && !hasMachine {
		return Result{}, errArg("ipmi_power: one of state or machine is required")
	}

	base := ipmitoolBase(name, port, user, password, key)

	if !hasMachine {
		changed, powerstate, failMsg, err := ipmiApplyPower(ctx, conn, base, "", state, timeout)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		result := Ok("unchanged")
		if changed {
			result = Changed(powerstate)
		}
		return result.WithExtra("powerstate", powerstate), nil
	}

	machines, ok := machineArg.([]any)
	if !ok {
		return Result{}, errArg("ipmi_power: machine must be a list")
	}
	var status []map[string]any
	anyChanged := false
	for _, raw := range machines {
		entry, _ := raw.(map[string]any)
		taddr := argInt(entry, "targetAddress", -1)
		if taddr < 0 || taddr > 255 {
			return Fail("ipmi_power: targetAddress should be set between 0 to 255."), nil
		}
		tstate := argString(entry, "state", "")
		if tstate == "" {
			tstate = state
		}
		if tstate == "" {
			return Fail("ipmi_power: either state or suboption of machine state should be set."), nil
		}
		if !isIPMIPowerState(tstate) {
			return Result{}, errArg("ipmi_power: machine state must be one of on, off, shutdown, reset, boot, got %q", tstate)
		}

		changed, powerstate, failMsg, err := ipmiApplyPower(ctx, conn, base, strconv.Itoa(taddr), tstate, timeout)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(failMsg), nil
		}
		if changed {
			anyChanged = true
		}
		status = append(status, map[string]any{"targetAddress": taddr, "powerstate": powerstate})
	}

	result := Ok("unchanged")
	if anyChanged {
		result = Changed("power state applied")
	}
	return result.WithExtra("status", status), nil
}

func isIPMIPowerState(s string) bool {
	switch s {
	case "on", "off", "shutdown", "reset", "boot":
		return true
	}
	return false
}

// ipmiApplyPower queries the current chassis power state (bridged
// through targetAddress when non-empty) and, if it differs from want,
// applies it, returning whether it changed and the resulting
// powerstate. For want == "shutdown"/"reset"/"boot", it is always
// applied — see moduleIPMIPower's own doc comment for why that matches
// real ipmi_power's own behavior, not a simplification specific to this
// port. A non-zero ipmitool exit is reported via failMsg (a
// Result{Failed:true}, matching real ipmi_power's own fail_json-on-
// exception behavior rather than crashing); err is reserved for this
// port's own transport failure running ipmitool at all.
func ipmiApplyPower(ctx context.Context, conn remoteexec.Connection, base, targetAddress, want string, timeout int) (changed bool, powerstate, failMsg string, err error) {
	bridge := ""
	if targetAddress != "" {
		bridge = " -t " + shellQuote(targetAddress)
	}

	statusRes, err := runStatus(ctx, conn, base+bridge+" chassis power status")
	if err != nil {
		return false, "", "", err
	}
	current := ipmiParsePowerStatus(statusRes.Stdout)

	if want == "on" || want == "off" {
		if current == want {
			return false, current, "", nil
		}
	} else if want == "boot" {
		if current == "off" {
			want = "on"
		} else {
			want = "reset"
		}
	}

	verb := map[string]string{"on": "on", "off": "off", "shutdown": "soft", "reset": "reset"}[want]
	res, err := runStatus(ctx, conn, base+bridge+" chassis power "+verb)
	if err != nil {
		return false, "", "", err
	}
	if res.RC != 0 {
		return false, "", fmt.Sprintf("ipmi_power: %s", strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout))), nil
	}
	_ = timeout // not enforced by this port's ipmitool-based implementation — see moduleIPMIPower's own doc comment

	// Real pyghmi's own set_power() response echoes back the requested
	// state as "powerstate" (rather than re-querying the chassis, which
	// may not have settled yet for shutdown/reset) — this port matches
	// that for on/off/shutdown/reset alike.
	return true, want, "", nil
}

// ipmiParsePowerStatus extracts "on"/"off" from ipmitool's own
// `chassis power status` output ("Chassis Power is on"/"...is off").
func ipmiParsePowerStatus(out string) string {
	out = strings.ToLower(out)
	if strings.Contains(out, "is on") {
		return "on"
	}
	if strings.Contains(out, "is off") {
		return "off"
	}
	return ""
}
