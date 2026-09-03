package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLvmPv implements Ansible's `lvm_pv` module: creates, resizes,
// or removes an LVM physical volume via `pvcreate`/`pvresize`/
// `pvremove`.
//
// Args: device (string, required); state (present|absent, default
// "present"); force (bool, default false) — `pvcreate -f` when
// creating, `pvremove -ff` when removing (matching real lvm_pv's own
// documented "-ff" for removing a PV that's still part of a VG); resize
// (bool, default false) — when state=present and the PV already exists,
// runs `pvresize` to grow it to the underlying device's current size.
//
// Existence is checked via `pvs --noheadings -o pv_name device`. When
// creating (state=present, no existing PV), this port also checks that
// device itself exists first, matching real lvm_pv's own documented
// "Device path must exist when creating a PV" note — reported as a
// normal Result{Failed:true} (the request is well-formed, it just can't
// be satisfied), not a Go error.
// Idempotency for resize: this port always issues `pvresize` when
// resize=true (real lvm_pv itself compares pv_size against the device's
// current size to decide whether a resize is a no-op; this port does
// not read the device's raw size independently to make that same
// comparison, so it defers to pvresize's own idempotency — pvresize
// is itself a no-op, exiting 0 with no change, when the PV is already
// at the device's full size — and reports Changed whenever resize=true
// was requested, which can over-report `changed` on a rerun where
// pvresize genuinely did nothing). This is a real (if minor) fidelity
// gap versus real lvm_pv's own more precise changed-detection, called
// out here rather than silently claimed as exact.
func moduleLvmPv(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	device, err := requireString(args, "device")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	force := argBool(args, "force", false)

	exists, err := lvmPvExists(ctx, conn, device)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "absent":
		if !exists {
			return Ok(device + " already has no PV signature"), nil
		}
		cmd := "pvremove"
		if force {
			cmd += " -ff"
		}
		cmd += " " + shellQuote(device)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(device + " PV removed"), nil

	case "present":
		resize := argBool(args, "resize", false)
		if !exists {
			exists2, err := pathExists(ctx, conn, device)
			if err != nil {
				return Result{}, err
			}
			if !exists2 {
				return Fail("lvm_pv: device " + device + " does not exist"), nil
			}
			cmd := "pvcreate"
			if force {
				cmd += " -f"
			}
			cmd += " " + shellQuote(device)
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			if resize {
				if _, err := run(ctx, conn, "pvresize "+shellQuote(device)); err != nil {
					return Result{}, err
				}
			}
			return Changed(device + " PV created"), nil
		}
		if resize {
			if _, err := run(ctx, conn, "pvresize "+shellQuote(device)); err != nil {
				return Result{}, err
			}
			return Changed(device + " PV resized"), nil
		}
		return Ok(device + " already a PV"), nil

	default:
		return Result{}, errArg("lvm_pv: state must be present or absent, got %q", state)
	}
}

func lvmPvExists(ctx context.Context, conn remoteexec.Connection, device string) (bool, error) {
	res, err := runStatus(ctx, conn, "pvs --noheadings -o pv_name "+shellQuote(device)+" 2>/dev/null")
	if err != nil {
		return false, err
	}
	return res.RC == 0 && strings.TrimSpace(res.Stdout) != "", nil
}
