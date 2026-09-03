package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleXfconfInfo implements Ansible's `xfconf_info`
// (community.general) module: read-only queries of XFCE 4 configuration
// via the `xfconf-query` CLI tool — see xfconf.go's own doc comment for
// why this port shells out to the real binary directly rather than
// substituting an architectural stand-in (there is no HTTP/library form
// to substitute here; real xfconf_info shells out to the same binary).
//
// Args: channel (optional — omitted lists every channel via
// `xfconf-query --list`); property (optional, requires channel —
// omitted with a channel given lists that channel's own properties via
// `xfconf-query --channel C --list`; given, reads that property's value
// via xfconf.go's own shared xfconfRead).
//
// Extra: channels ([]string, only when channel is omitted); properties
// ([]string, only when channel is given without property); value
// (string, empty for an array-typed property) and value_array
// ([]string, empty for a scalar) and is_array (bool), only when both
// channel and property are given; version (`xfconf-query --version`,
// always).
//
// Never Changed — this module only ever reads, matching real
// xfconf_info's own documented check_mode support (full, "This action
// does not modify state").
func moduleXfconfInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	channel := argString(args, "channel", "")
	property := argString(args, "property", "")
	if property != "" && channel == "" {
		return Result{}, errArg("xfconf_info: channel is required when property is given")
	}

	version, err := xfconfVersion(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	r := Ok("").WithExtra("version", version)

	if channel == "" {
		res, err := runStatus(ctx, conn, "xfconf-query --list")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("xfconf_info: unable to list channels: " + strings.TrimSpace(res.Stderr)), nil
		}
		return r.WithExtra("channels", xfconfLines(res.Stdout)), nil
	}

	if property == "" {
		res, err := runStatus(ctx, conn, "xfconf-query --channel "+shellQuote(channel)+" --list")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("xfconf_info: unable to list properties for channel " + channel + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return r.WithExtra("properties", xfconfLines(res.Stdout)), nil
	}

	isArray, scalar, values, exists, err := xfconfRead(ctx, conn, channel, property)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Fail("xfconf_info: property " + property + " does not exist on channel " + channel), nil
	}
	if values == nil {
		values = []string{}
	}
	return r.WithExtra("is_array", isArray).WithExtra("value", scalar).WithExtra("value_array", values), nil
}

// xfconfLines splits `xfconf-query --list` output (one channel/property
// per line) into a []string, dropping empty lines.
func xfconfLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	if lines == nil {
		lines = []string{}
	}
	return lines
}
