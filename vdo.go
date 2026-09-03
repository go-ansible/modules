package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleVdo implements (a subset of) Ansible's `vdo` module: creates,
// removes, or reconfigures a Linux VDO (Virtual Data Optimizer)
// deduplication volume via the `vdo` CLI tool.
//
// Args: name (string, required); state (present|absent, default
// "present"); device (string, required for state=present when
// creating); force (bool, default false) — `vdo create --force`, or
// `vdo stop --force`/`vdo remove --force` when removing/stopping;
// running (bool) — starts (`vdo start`) or stops (`vdo stop`) the
// volume; activated (bool) — `vdo activate`/`vdo deactivate`.
// Everything else (ackthreads, biothreads, blockmapcachesize,
// compression, cputhreads, deduplication, emulate512, growphysical,
// indexmem, indexmode, logicalsize, logicalthreads, physicalthreads,
// readcache, readcachesize, slabsize, writepolicy) is passed straight
// through to `vdo create`'s own like-named `--<arg>` flag when
// creating a NEW volume, matching real vdo's own flag-per-argument
// design one for one; a handful of these (emulate512, indexmem,
// indexmode, slabsize) are real vdo's own documented "only available
// when creating" options and this port likewise never re-applies them
// to an existing volume.
//
// For an EXISTING volume, this port re-applies only the arguments real
// vdo's own `vdo modify`/`vdo start`/`vdo activate` commands actually
// support changing post-creation: compression/deduplication (`vdo
// enableCompression`/`disableCompression`/`enableDeduplication`/
// `disableDeduplication`), logicalsize/blockmapcachesize/
// readcachesize/writepolicy/ackthreads/biothreads/cputhreads/
// logicalthreads/physicalthreads (`vdo modify --<flag>`), running
// (`vdo start`/`vdo stop`), activated (`vdo activate`/`vdo
// deactivate`), and growphysical (`vdo growPhysical`, only attempted
// when growphysical=true — this port does NOT itself check the "at
// least 64GB free" precondition real vdo's own module applies before
// attempting a grow; `vdo growPhysical` fails on its own with a clear
// error if there isn't enough room, which surfaces as this module's
// own command failure).
//
// Idempotency: this port has no portable, scriptable way to read VDO's
// own current per-argument configuration back out the way `vdo status`
// prints it in a form worth parsing field-by-field for every one of the
// arguments above (its YAML-like structure is real vdo's own, undocumented
// beyond `vdo status`'s own output). Rather than parsing that output
// unreliably, this port checks ONLY WHETHER name currently exists (via
// `vdo status --name=name`, exit 0 iff it does) to decide create vs.
// modify, and then — for an EXISTING volume — always issues `vdo
// modify`/`enableCompression`/etc for every argument actually given,
// reporting Changed whenever any such command was run. This means an
// existing volume's rerun with the SAME arguments as last time reports
// Changed even though nothing observably changed on the target — a
// real (if conservative) fidelity gap versus real vdo's own more
// precise per-field idempotency, called out here rather than silently
// claimed as exact.
func moduleVdo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	force := argBool(args, "force", false)

	exists, err := vdoExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(name + " already absent"), nil
		}
		cmd := "vdo remove --name=" + shellQuote(name)
		if force {
			cmd += " --force"
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil
	}
	if state != "present" {
		return Result{}, errArg("vdo: state must be present or absent, got %q", state)
	}

	changed := false
	justCreated := false
	if !exists {
		device, err := requireString(args, "device")
		if err != nil {
			return Result{}, errArg("vdo: device is required to create %q", name)
		}
		cmd := "vdo create --name=" + shellQuote(name) + " --device=" + shellQuote(device)
		if force {
			cmd += " --force"
		}
		for _, flag := range append(append([]string{}, vdoCreateOnlyFlags...), vdoModifiableFlags...) {
			if v := argString(args, flag, ""); v != "" {
				cmd += " --" + flag + "=" + shellQuote(v)
			}
		}
		if argBool(args, "emulate512", false) {
			cmd += " --emulate512=enabled"
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
		justCreated = true
	}

	if !justCreated {
		for _, flag := range vdoModifiableFlags {
			v := argString(args, flag, "")
			if v == "" {
				continue
			}
			if _, err := run(ctx, conn, "vdo modify --name="+shellQuote(name)+" --"+flag+"="+shellQuote(v)); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if v, ok := args["compression"]; ok {
		sub := "enableCompression"
		if v == "disabled" {
			sub = "disableCompression"
		}
		if _, err := run(ctx, conn, "vdo "+sub+" --name="+shellQuote(name)); err != nil {
			return Result{}, err
		}
		changed = true
	}
	if v, ok := args["deduplication"]; ok {
		sub := "enableDeduplication"
		if v == "disabled" {
			sub = "disableDeduplication"
		}
		if _, err := run(ctx, conn, "vdo "+sub+" --name="+shellQuote(name)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if _, ok := args["growphysical"]; ok && argBool(args, "growphysical", false) {
		if _, err := run(ctx, conn, "vdo growPhysical --name="+shellQuote(name)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if _, ok := args["running"]; ok {
		sub := "start"
		if !argBool(args, "running", true) {
			sub = "stop"
		}
		cmd := "vdo " + sub + " --name=" + shellQuote(name)
		if sub == "stop" && force {
			cmd += " --force"
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	}
	if _, ok := args["activated"]; ok {
		sub := "activate"
		if !argBool(args, "activated", true) {
			sub = "deactivate"
		}
		if _, err := run(ctx, conn, "vdo "+sub+" --name="+shellQuote(name)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if changed {
		return Changed(name + " updated"), nil
	}
	return Ok(name + " already up to date"), nil
}

// vdoCreateOnlyFlags are real vdo's own "only available when creating
// a new volume, cannot be changed for an existing volume" arguments,
// per this module's doc comment.
var vdoCreateOnlyFlags = []string{"indexmem", "indexmode", "slabsize"}

// vdoModifiableFlags are passed to `vdo modify` for an existing
// volume (and to `vdo create` for a new one, via the loop that also
// includes vdoCreateOnlyFlags at creation time — see moduleVdo).
var vdoModifiableFlags = []string{
	"logicalsize", "blockmapcachesize", "readcachesize", "readcache",
	"writepolicy", "ackthreads", "biothreads", "cputhreads",
	"logicalthreads", "physicalthreads",
}

func vdoExists(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "vdo status --name="+shellQuote(name)+" >/dev/null 2>&1")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
