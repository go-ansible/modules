package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAixDevices implements (a subset of) Ansible's `aix_devices`
// module (community.general, deprecated upstream in favor of
// `ibm.power_aix.devices`, but still a real community.general module):
// discovers, defines, removes, and changes attributes of AIX devices
// via `cfgmgr`/`rmdev`/`chdev`/`lsdev`/`lsattr`.
//
// Args: device (string, optional) — the device name, or "all" to
// rescan every device; attributes (map[string]any, default none) — a
// set of device attribute=value pairs to apply via `chdev`, applied
// instead of discovery when non-empty; force (bool, default false) —
// `chdev -l device -a attr=val -f` / `rmdev ... -f`; recursive (bool,
// default false) — `rmdev -R`; state (available|defined|removed,
// default "available") — "available" (re)discovers/configures a
// device (or changes its attributes, when `attributes` is given);
// "defined" and "removed" both operate on an existing device via
// `rmdev`, requiring `device`.
//
// A device's existence and current state are read via `lsdev -C -l
// device` (second whitespace field is the AIX device state, e.g.
// "Available"/"Defined"); an attribute's current value via `lsattr -El
// device -a attr` (exit 255 means the attribute doesn't apply to this
// device — treated as invalid, except `delalias4`/`delalias6`, which
// real aix_devices treats as an always-different empty current value,
// matching its own hidden_attrs special-case).
//
// Two deviations from real aix_devices.py's own command construction,
// both DELIBERATE (not gaps in what this port's architecture could
// replicate): (1) its discover_device() builds the `-l <device>` flag
// as a SINGLE Python string (`f"-l {device}"`) that it then passes as
// ONE element of the argv list to run_command() — meaning the real
// module hands cfgmgr a single malformed token ("-l vio0", not `-l`
// and `vio0` as two argv entries) for every device-scoped scan,
// including its own "device: all" example (which becomes "-l all"
// rather than a plain unqualified scan); this port instead runs
// `cfgmgr -l <device>` as the clearly-intended two tokens (and no `-l`
// at all when device is empty or "all"), since shell composition here
// naturally produces that anyway and reproducing an apparent upstream
// defect would make this port less useful without adding real
// fidelity. (2) its remove_device() computes a `-d` flag string from
// state (`"-d"` for removed/absent, `""` for defined) but that
// computed string is only ever used to CHOOSE between two nearly
// identical run_command() argv lists — the `-d` value itself is never
// actually appended to either — so real aix_devices's own state=
// removed does not appear to pass `-d` to `rmdev` at all, meaning it
// likely only defines the device (same effect as state=defined)
// rather than truly removing it, despite its own documented "removed
// (alias absent) removes a device"; this port passes `-d` for
// state=removed, matching the documented intent, and omits it for
// state=defined.
func moduleAixDevices(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	device := argString(args, "device", "")
	force := argBool(args, "force", false)
	recursive := argBool(args, "recursive", false)
	state := argString(args, "state", "available")
	attrs := aixDevicesAttrsArg(args)

	forceFlag := ""
	if force {
		forceFlag = "-f"
	}
	recursiveFlag := ""
	if recursive {
		recursiveFlag = "-R"
	}

	switch state {
	case "available", "present":
		if len(attrs) > 0 {
			exists, _, err := aixDeviceStatus(ctx, conn, device)
			if err != nil {
				return Result{}, err
			}
			if !exists {
				return Ok(fmt.Sprintf("Device %s does not exist.", device)), nil
			}
			return aixDevicesChangeAttrs(ctx, conn, device, attrs, forceFlag)
		}
		if device != "" && device != "all" {
			exists, _, err := aixDeviceStatus(ctx, conn, device)
			if err != nil {
				return Result{}, err
			}
			if !exists {
				return Ok(fmt.Sprintf("Device %s does not exist.", device)), nil
			}
		}
		return aixDevicesDiscover(ctx, conn, device)

	case "removed", "absent", "defined":
		if device == "" {
			return Fail("device is required to removed or defined state."), nil
		}
		exists, devState, err := aixDeviceStatus(ctx, conn, device)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(fmt.Sprintf("Device %s does not exist.", device)), nil
		}
		if state == "defined" && devState == "Defined" {
			return Ok(fmt.Sprintf("Device %s already in Defined", device)), nil
		}
		return aixDevicesRemove(ctx, conn, device, state, forceFlag, recursiveFlag)

	default:
		return Result{}, errArg("aix_devices: state must be available, defined, or removed, got %q", state)
	}
}

func aixDevicesAttrsArg(args map[string]any) map[string]string {
	out := map[string]string{}
	raw, _ := args["attributes"].(map[string]any)
	for k, v := range raw {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func aixDeviceStatus(ctx context.Context, conn remoteexec.Connection, device string) (exists bool, state string, err error) {
	res, err := runStatus(ctx, conn, "lsdev -C -l "+shellQuote(device))
	if err != nil {
		return false, "", err
	}
	if res.RC != 0 {
		return false, "", fmt.Errorf("aix_devices: lsdev failed: %s", strings.TrimSpace(res.Stderr))
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return false, "", nil
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return true, "", nil
	}
	return true, fields[1], nil
}

func aixDeviceAttrCurrent(ctx context.Context, conn remoteexec.Connection, device, attr string) (value string, invalid bool, err error) {
	res, err := runStatus(ctx, conn, "lsattr -El "+shellQuote(device)+" -a "+shellQuote(attr))
	if err != nil {
		return "", false, err
	}
	if res.RC == 255 {
		if attr == "delalias4" || attr == "delalias6" {
			return "", false, nil
		}
		return "", true, nil
	}
	if res.RC != 0 {
		return "", false, fmt.Errorf("aix_devices: lsattr failed: %s", strings.TrimSpace(res.Stderr))
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) < 2 {
		return "", false, nil
	}
	return fields[1], false, nil
}

func aixDevicesChangeAttrs(ctx context.Context, conn remoteexec.Connection, device string, attrs map[string]string, forceFlag string) (Result, error) {
	var changed, unchanged, invalid []string
	for _, attr := range zfsSortedKeys(attrs) {
		val := attrs[attr]
		cur, isInvalid, err := aixDeviceAttrCurrent(ctx, conn, device, attr)
		if err != nil {
			return Result{}, err
		}
		if isInvalid {
			invalid = append(invalid, attr)
			continue
		}
		if cur == val {
			unchanged = append(unchanged, val)
			continue
		}
		cmd := "chdev -l " + shellQuote(device) + " -a " + shellQuote(attr+"="+val)
		if forceFlag != "" {
			cmd += " " + forceFlag
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = append(changed, val)
	}

	var msg strings.Builder
	if len(changed) > 0 {
		msg.WriteString("Attributes changed: " + strings.Join(changed, ",") + ". ")
	}
	if len(unchanged) > 0 {
		msg.WriteString("Attributes already set: " + strings.Join(unchanged, ",") + ". ")
	}
	if len(invalid) > 0 {
		msg.WriteString("Invalid attributes: " + strings.Join(invalid, ", ") + " ")
	}
	out := strings.TrimSpace(msg.String())
	if len(changed) > 0 {
		return Changed(out), nil
	}
	return Ok(out), nil
}

func aixDevicesDiscover(ctx context.Context, conn remoteexec.Connection, device string) (Result, error) {
	cmd := "cfgmgr"
	if device != "" && device != "all" {
		cmd += " -l " + shellQuote(device)
	}
	out, err := run(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	return Changed(out), nil
}

func aixDevicesRemove(ctx context.Context, conn remoteexec.Connection, device, state, forceFlag, recursiveFlag string) (Result, error) {
	cmd := "rmdev -l " + shellQuote(device)
	if recursiveFlag != "" {
		cmd += " " + recursiveFlag
	}
	if state == "removed" || state == "absent" {
		cmd += " -d"
		if forceFlag != "" {
			cmd += " " + forceFlag
		}
	}
	out, err := run(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	return Changed(out), nil
}
