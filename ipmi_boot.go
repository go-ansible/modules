package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIPMIBoot implements (a subset of) Ansible's `ipmi_boot`
// module: sets a machine's next-boot device over IPMI.
//
// Architectural note, and why it matters here: real ipmi_boot does NOT
// shell out to any CLI at all — it uses the `pyghmi` Python library
// (see real module's own REQUIREMENTS: pyghmi) to speak the IPMI-over-
// LAN(+) protocol directly in-process, from wherever the module runs,
// to the BMC named by `name`/`port` (a `command.Command(bmc=name,
// userid=user, password=password, port=port, kg=key)` object). This is
// genuinely a controller/jump-host-to-remote-BMC operation, NOT a
// "run this against the inventory host's own local BMC" one — nothing
// in this port's architecture (a Connection reached via Exec/Put/
// Fetch/Remove) has an IPMI-over-LAN protocol implementation, and
// implementing RMCP+ session establishment/encryption from scratch in
// Go is out of scope for this port. Instead — matching uri.go/
// apache2_mod_proxy.go's own established convention for a module that
// needs to reach a network service this port has no client library
// for — this module shells out to `ipmitool` (a real, ubiquitous CLI
// that speaks the identical protocol pyghmi does) via conn.Exec,
// targeting the named BMC over the network with `ipmitool -I lanplus
// -H <name> -p <port> ...` exactly as pyghmi itself would. The
// requirement this port actually has is therefore `ipmitool` installed
// on whatever host conn reaches (typically the control node itself, or
// a jump host with BMC network access — matching real ipmi_boot's own
// typical `delegate_to: localhost`/bastion usage), not `pyghmi`.
//
// Args: name (string, required) — BMC hostname/IP. port (int, default
// 623). user/password (string, required). key (string, optional) —
// hex-encoded BMC "Kg" key, passed as ipmitool's `-y`. bootdev (string,
// required, one of network|floppy|hd|safe|optical|setup|default) —
// mapped to ipmitool's own device names (network->pxe, hd->disk,
// optical->cdrom, setup->bios, default->none; floppy/safe pass
// through unchanged). state (present|absent, default "present") —
// present requests bootdev; absent means "this bootdev must NOT be the
// current request", which real ipmi_boot achieves by resetting to
// "default"/"none" ONLY if the currently-set device is bootdev (real
// module's own `elif state == "absent" and current["bootdev"] ==
// bootdev: request = dict(bootdev="default")`, a no-op otherwise) — this
// port does not read the current boot device back first (see
// Simplifications below) and instead always resets to "none" for
// state=absent, unconditionally. state=absent with bootdev=default is
// rejected, matching real ipmi_boot's own validation ("The bootdev
// 'default' cannot be used with state 'absent'."). persistent (bool,
// default false) — ipmitool
// `options=persistent`. uefiboot (bool, default false) — ipmitool
// `options=efiboot`.
//
// Simplifications vs real ipmi_boot: real ipmi_boot first calls
// `get_bootdev()` and only issues `set_bootdev()` if the request would
// actually change something, reporting Changed=false for a true no-op.
// This port has no equivalent read for ipmitool (parsing `ipmitool
// chassis bootparam get 5`'s raw boot-flags hex is well beyond what
// this port's other idempotency checks attempt) and instead always
// issues `ipmitool chassis bootdev` and reports Changed=true on
// success — the same "a no-op call still exits 0, and parsing the
// tool's own output to detect that cheaply isn't worth it" tradeoff
// apt.go's own `state: latest` branch documents, applied here. This
// port has no check_mode support at all (a runtime-engine concern
// outside every module's own Func signature here).
func moduleIPMIBoot(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
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
	bootdev, err := requireString(args, "bootdev")
	if err != nil {
		return Result{}, err
	}
	ipmiDev, ok := ipmiBootdevMap[bootdev]
	if !ok {
		return Result{}, errArg("ipmi_boot: bootdev must be one of network, floppy, hd, safe, optical, setup, default, got %q", bootdev)
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ipmi_boot: state must be present or absent, got %q", state)
	}
	if state == "absent" && bootdev == "default" {
		return Fail("ipmi_boot: the bootdev 'default' cannot be used with state 'absent'."), nil
	}
	port := argInt(args, "port", 623)
	key := argString(args, "key", "")
	persistent := argBool(args, "persistent", false)
	uefiboot := argBool(args, "uefiboot", false)

	dev := ipmiDev
	if state == "absent" {
		dev = "none"
	}

	var opts []string
	if persistent {
		opts = append(opts, "persistent")
	}
	if uefiboot {
		opts = append(opts, "efiboot")
	}

	cmd := ipmitoolBase(name, port, user, password, key) + " chassis bootdev " + shellQuote(dev)
	if len(opts) > 0 {
		cmd += " options=" + strings.Join(opts, ",")
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("ipmi_boot: %s", strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout)))), nil
	}

	result := Changed(dev)
	result = result.WithExtra("bootdev", bootdev)
	result = result.WithExtra("persistent", persistent)
	result = result.WithExtra("uefimode", uefiboot)
	return result, nil
}

// ipmiBootdevMap translates real ipmi_boot's own `bootdev` choices to
// ipmitool's own `chassis bootdev` device names.
var ipmiBootdevMap = map[string]string{
	"network": "pxe",
	"floppy":  "floppy",
	"hd":      "disk",
	"safe":    "safe",
	"optical": "cdrom",
	"setup":   "bios",
	"default": "none",
}

// ipmitoolBase composes the shared `ipmitool -I lanplus -H ... ` prefix
// used by ipmi_boot.go/ipmi_power.go to reach a remote BMC over the
// network — see moduleIPMIBoot's own doc comment for why this port
// shells out to ipmitool rather than using pyghmi as real ipmi_boot/
// ipmi_power do.
func ipmitoolBase(name string, port int, user, password, key string) string {
	cmd := fmt.Sprintf("ipmitool -I lanplus -H %s -p %d -U %s -P %s",
		shellQuote(name), port, shellQuote(user), shellQuote(password))
	if key != "" {
		cmd += " -y " + shellQuote(key)
	}
	return cmd
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
