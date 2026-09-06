package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what hpilo_boot.go and hpilo_info.go share.
//
// Real hpilo_boot.py/hpilo_info.py talk to the iLO's own network address
// via the `hpilo` Python library's RIBCL protocol (host/login/password
// pointed AT the iLO) — RIBCL predates Redfish, but the same physical
// iLO/`ilorest` combination this port already uses for ilo_redfish_*
// (see ilo_common.go's own doc comment) covers the same real hardware
// behavior these two modules need — one-time boot device selection,
// power control, basic hardware/network info — via Redfish instead,
// verified against HPE's own python-redfish-utility source
// (RebootCommand.py, BootOrderCommand.py) before writing this file.
// This port therefore runs ilorest LOCAL/in-band exactly as
// redfish_common.go's own doc comment describes: host/login/password
// are accepted for argument-shape compatibility but have NO EFFECT.
//
// # Boot device mapping
//
// Real hpilo_boot's `media` choices (cdrom, floppy, rbsu, hdd, network,
// normal, usb) map onto ilorest's own `bootorder --onetimeboot=<X>`
// device enum (confirmed via HPE's own Set_One_Time_Boot_Order.sh
// example script: None, Cd, Hdd, Usb, Utilities, Diags, BiosSetup, Pxe,
// UefiShell, UefiTarget, plus iLO5-only SDCard/UefiHttp) as follows.
// `floppy` has NO Redfish/ilorest equivalent at all — virtual floppy
// boot is a RIBCL-era concept with no BootSourceOverrideTarget value on
// modern iLO firmware — so this port fails loud for `media: floppy`
// rather than silently mapping it to something else.
var hpiloMediaToBootOrder = map[string]string{
	"cdrom":   "Cd",
	"hdd":     "Hdd",
	"usb":     "Usb",
	"network": "Pxe",
	"normal":  "None",
	"rbsu":    "BiosSetup",
}

func hpiloRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	return redfishRequireBinary(ctx, conn, moduleName, "ilorest",
		"this port shells out to HPE's own local ilorest (RESTful Interface Tool) CLI rather than "+
			"speaking RIBCL directly — see hpilo_common.go's own doc comment")
}

// hpiloSetOneTimeBoot runs `ilorest bootorder --onetimeboot=<device>`
// then `ilorest commit` in one Exec call (ilorest stages a bootorder
// change locally and requires a separate commit to flush it — see
// HPE's own Set_One_Time_Boot_Order.sh example, which runs the same two
// commands back to back).
func hpiloSetOneTimeBoot(ctx context.Context, conn remoteexec.Connection, device string) (remoteexec.Result, error) {
	return runStatus(ctx, conn, "ilorest bootorder --onetimeboot="+shellQuote(device)+" && ilorest commit")
}

// hpiloReboot runs `ilorest reboot <arg>`, arg being one of the exact
// values RebootCommand.py itself validates (On, ForceOff, ForceRestart,
// Nmi, PushPowerButton, Press, PressAndHold, ColdBoot) — read directly
// from HPE's own source before writing this file, not guessed.
func hpiloReboot(ctx context.Context, conn remoteexec.Connection, arg string) (remoteexec.Result, error) {
	return runStatus(ctx, conn, "ilorest reboot "+arg)
}

// hpiloPowerState runs a rawget on the system resource (via the same
// iloSystemURI collection walk ilo_redfish_command.go already uses —
// never a hardcoded "/redfish/v1/Systems/1/", since the exact member ID
// varies across iLO generations) and returns Redfish's PowerState
// ("On"/"Off") upper-cased to "ON"/"OFF"/"UNKNOWN", matching real
// hpilo_boot/hpilo_info's own RIBCL-derived string convention exactly
// (see hpilo_info.py's RETURN doc: V(ON), V(OFF), V(UNKNOWN)).
func hpiloPowerState(ctx context.Context, conn remoteexec.Connection) (string, remoteexec.Result, error) {
	systemURI, err := iloSystemURI(ctx, conn)
	if err != nil {
		return "", remoteexec.Result{}, err
	}
	if systemURI == "" {
		return "UNKNOWN", remoteexec.Result{}, nil
	}
	var sys struct {
		PowerState string `json:"PowerState"`
	}
	res, err := iloRawGet(ctx, conn, systemURI, &sys)
	if err != nil {
		return "", res, err
	}
	if res.RC != 0 || sys.PowerState == "" {
		return "UNKNOWN", res, nil
	}
	return strings.ToUpper(sys.PowerState), res, nil
}
