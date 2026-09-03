package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUsbFacts implements Ansible's `usb_facts` (community.general)
// module: gathers connected USB devices into Extra["usb_devices"] (a
// list of maps, matching real usb_facts' own ansible_facts.usb_devices
// shape) by parsing `lsusb`'s default one-line-per-device output —
// mirroring service_facts.go/mount_facts.go's own read-only,
// Extra-populating shape (see those files' doc comments for why this
// port's facts modules use Extra rather than the Facts field: this
// package's house convention keeps ansible_facts merging at the
// caller/engine layer, not inside individual module functions).
//
// Args: none.
//
// Each `lsusb` line has the fixed form:
//
//	Bus 001 Device 002: ID 1d6b:0002 Linux Foundation 2.0 root hub
//
// parsed into bus ("001"), device ("002"), id ("1d6b:0002"), and name
// (everything after the id, matching real usb_facts' own field names
// and samples exactly).
//
// Simplifications vs real usb_facts: none known — real usb_facts'
// own Python implementation does the same fixed-format regex parse of
// plain `lsusb` output and exposes exactly these four fields, no more.
func moduleUsbFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	res, err := runStatus(ctx, conn, "command -v lsusb >/dev/null 2>&1")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("usb_facts: lsusb not found on PATH (install the usbutils package)"), nil
	}

	out, err := run(ctx, conn, "lsusb")
	if err != nil {
		return Result{}, err
	}

	devices := parseLsusb(out)
	return Ok("").WithExtra("usb_devices", devices), nil
}

// parseLsusb parses plain `lsusb` output ("Bus BBB Device DDD: ID
// vvvv:pppp Name...") into a list of maps with bus/device/id/name.
// Lines that don't match this shape are skipped.
func parseLsusb(out string) []map[string]any {
	var devices []map[string]any
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] != "Bus" || fields[2] != "Device" || fields[4] != "ID" {
			continue
		}
		bus := fields[1]
		device := strings.TrimSuffix(fields[3], ":")
		id := fields[5]
		name := strings.TrimSpace(strings.Join(fields[6:], " "))
		devices = append(devices, map[string]any{
			"bus":    bus,
			"device": device,
			"id":     id,
			"name":   name,
		})
	}
	return devices
}
