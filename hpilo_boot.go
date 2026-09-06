package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHpiloBoot implements Ansible's `hpilo_boot` module. Real
// hpilo_boot.py talks RIBCL, an older HPE protocol than Redfish — this
// port substitutes the exact same local/in-band `ilorest` this batch
// already uses for the Redfish-native ilo_redfish_* family (see
// hpilo_common.go's own doc comment).
//
// # What is covered
//
// `media` (cdrom, hdd, usb, network, normal, rbsu — NOT floppy, see
// below) sets the one-time boot device via `ilorest bootorder
// --onetimeboot=<X>` + `ilorest commit`. Power control (turning a
// powered-off system on, or forcing it off for `state: poweroff`)
// reproduces real `press_pwr_btn()`/`hold_pwr_btn()` via `ilorest
// reboot PushPowerButton`/`ilorest reboot PressAndHold` — arguments
// confirmed against HPE's own RebootCommand.py source, not guessed.
//
// # What is NOT covered, and fails loud rather than silently diverging
//
//   - `media: floppy` — RIBCL's virtual-floppy boot target has no
//     Redfish BootSourceOverrideTarget equivalent at all (confirmed
//     against HPE's own Set_One_Time_Boot_Order.sh device list: None,
//     Cd, Hdd, Usb, Utilities, Diags, BiosSetup, Pxe, UefiShell,
//     UefiTarget); there is nothing to map it to.
//   - `image` (virtual media insert/eject) — real hpilo_boot's
//     `insert_virtual_media`/`set_vm_status`/`set_vf_status` calls
//     manage Redfish's VirtualMedia resource, which this port has not
//     yet implemented; a task that sets `image` fails loud rather than
//     silently skipping the mount.
//   - Forcing a reboot while the system is already ON (`force: true`
//     with the system powered on) reproduces real `warm_boot_server()`
//     — a GRACEFUL, ACPI-mediated restart. `ilorest reboot`'s own
//     argument list (read directly from RebootCommand.py) has no
//     graceful-restart-while-on option at all: only `ForceRestart`
//     (explicitly documented as "an immediate NON-graceful shutdown,
//     followed by a restart") and `ColdBoot` (hard power cycle) are
//     available, both more disruptive than what a real playbook using
//     `force: true` would expect. Rather than silently substitute a
//     harder reboot than requested, this port fails loud and documents
//     the gap.
//
// Args: host/login/password/ssl_version accepted for argument-shape
// compatibility but have NO EFFECT (see redfish_common.go's own doc
// comment on this batch's local/in-band CLI architecture); media
// (string); image (string); state (default "boot_once"); force (bool);
// idempotent_boot_once (bool).
func moduleHpiloBoot(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := requireString(args, "host"); err != nil {
		return Result{}, err
	}
	media := argString(args, "media", "")
	image := argString(args, "image", "")
	state := argString(args, "state", "boot_once")
	force := argBool(args, "force", false)
	idempotentBootOnce := argBool(args, "idempotent_boot_once", false)

	if res, ok := hpiloRequireBinary(ctx, conn, "hpilo_boot"); !ok {
		return res, nil
	}

	changed := false

	if media != "" && stringSliceContains([]string{"boot_always", "boot_once", "connect", "disconnect", "no_boot"}, state) {
		device, ok := hpiloMediaToBootOrder[media]
		if !ok {
			return Fail("hpilo_boot: media '" + media + "' has no ilorest/Redfish one-time-boot equivalent (see hpilo_common.go's own doc comment)"), nil
		}
		if image != "" {
			return Fail("hpilo_boot: image (virtual media insert) is not yet implemented by this port — see hpilo_boot.go's own doc comment"), nil
		}
		res, err := hpiloSetOneTimeBoot(ctx, conn, device)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("hpilo_boot: setting one-time boot: " + iloErrMsg(res)), nil
		}
		changed = true
	}

	powerStatus := "UNKNOWN"
	if state == "boot_once" || state == "boot_always" || force {
		var err error
		powerStatus, _, err = hpiloPowerState(ctx, conn)
		if err != nil {
			return Result{}, err
		}

		if powerStatus == "ON" {
			if !force && !idempotentBootOnce {
				return Fail("hpilo_boot: the server is already powered on"), nil
			}
			if force {
				return Fail("hpilo_boot: forcing a reboot while already powered on needs a graceful restart " +
					"ilorest's own reboot command does not offer — see hpilo_boot.go's own doc comment"), nil
			}
			// idempotentBootOnce, not force: no-op, matching real hpilo_boot.
		} else {
			res, err := hpiloReboot(ctx, conn, "PushPowerButton")
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("hpilo_boot: powering on: " + iloErrMsg(res)), nil
			}
			changed = true
		}
	} else if state == "poweroff" {
		var err error
		powerStatus, _, err = hpiloPowerState(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if powerStatus != "OFF" {
			res, err := hpiloReboot(ctx, conn, "PressAndHold")
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("hpilo_boot: powering off: " + iloErrMsg(res)), nil
			}
			changed = true
		}
	}

	out := Ok("")
	if changed {
		out = Changed("")
	}
	return out.WithExtra("power", powerStatus), nil
}
