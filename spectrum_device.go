package modules

import (
	"context"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSpectrumDevice implements Ansible's `spectrum_device`
// (community.general) module: creates or deletes a device model in CA
// Spectrum, via Broadcom's own official vnmsh CLI — see
// spectrum_common.go's own doc comment for the verified architecture
// mismatch (vnmsh is a LOCAL tool on the SpectroSERVER host, unlike
// real spectrum_device.py's own REMOTE OneClick REST calls) and the
// command syntax this port relies on.
//
// # Idempotency, matching real get_device()/add_device()/remove_device()
//
// Real spectrum_device.py's own add_device()/remove_device() both
// start by calling get_device(device_ip), which searches for an
// existing model by its Network_Address attribute (hex id 0x12d7f)
// within the given landscape's own handle range — this port's own
// spectrumSeek(ctx, conn, "0x12d7f", deviceIP, landscape) reproduces
// that exact lookup (by the SAME attribute id, scoped to the SAME
// landscape) via `./seek`, not a from-scratch invention.
//
// Args: device (required, aliases host/name — real module resolves a
// hostname to an IP via a DNS lookup on the CONTROL node; this port
// does not perform that resolution itself, since the target host and
// meaning of "resolve" differ once vnmsh runs directly on the
// SpectroSERVER — a caller wanting hostname resolution should resolve
// it themselves before calling this module); community (required when
// state=present); landscape (required); state (present|absent, default
// present); agentport (default 161) → `agentport=` on `./create`, only
// when non-default is meaningful to vnmsh (this port always sends it,
// matching real add_device()'s own unconditional
// `&agentport={agentport}` when set). url/url_username/url_password/
// use_proxy/validate_certs are accepted (real module marks the first
// three required) but not used to authenticate — see
// spectrum_common.go's own doc comment.
func moduleSpectrumDevice(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "spectrum_device"
	device := argString(args, "device", argString(args, "host", argString(args, "name", "")))
	if device == "" {
		return Result{}, errArg("%s: missing required argument: device (or host/name)", mod)
	}
	landscape, err := requireString(args, "landscape")
	if err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "url"); err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "url_username"); err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "url_password"); err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of [present, absent], got %q", mod, state)
	}

	if res, ok := spectrumRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}

	handle, found, err := spectrumSeek(ctx, conn, "0x12d7f", device, landscape)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !found {
			return Ok(mod + ": device " + device + " does not exist in landscape " + landscape), nil
		}
		res, err := spectrumRun(ctx, conn, "destroy", "model", "mh="+handle, "-n")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(mod + ": failed to destroy model " + handle + ": " + spectrumErrMsg(res)), nil
		}
		return Changed(""), nil
	}

	// state == present
	if found {
		return Ok(mod+": device "+device+" already exists").
			WithExtra("device", map[string]any{"model_handle": handle, "address": device, "landscape": landscape}), nil
	}
	community, err := requireString(args, "community")
	if err != nil {
		return Result{}, err
	}
	tokens := []string{"model", "ip=" + device, "comm=" + community, "lh=" + landscape}
	if port := argInt(args, "agentport", 161); port != 0 {
		tokens = append(tokens, "agentport="+strconv.Itoa(port))
	}
	res, err := spectrumRun(ctx, conn, "create", tokens...)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(mod + ": failed to create model for " + device + ": " + spectrumErrMsg(res)), nil
	}
	newHandle := spectrumHexHandleRE.FindString(res.Stdout)
	return Changed("").WithExtra("device", map[string]any{
		"model_handle": newHandle,
		"address":      device,
		"landscape":    landscape,
	}), nil
}
