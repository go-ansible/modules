package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLvg implements (a subset of) Ansible's `lvg` module: creates,
// removes, extends/reduces, or (de)activates an LVM volume group via
// `vgs`/`pvcreate`/`vgcreate`/`vgextend`/`vgreduce`/`vgremove`/
// `vgchange`.
//
// Args: vg (string, required); pvs (list of string) — the volume
// group's DESIRED set of physical volumes; required when creating a new
// VG. When the VG already exists, this is a desired-STATE list: a PV
// listed here but not yet in the VG is added (running `pvcreate` on it
// first if it isn't already a PV); a PV currently in the VG but NOT
// listed here is removed via `vgreduce`, UNLESS remove_extra_pvs=false
// (default true) — matching real lvg's own documented "physical volumes
// not listed here are removed from it by default"; pv_options (string,
// default "") — inserted verbatim into `pvcreate` (see filesystem.go's
// doc comment for this port's house convention on free-form opts
// arguments); vg_options (string, default "") — likewise for
// `vgcreate`; pesize (string, default "4") — `vgcreate --physicalextentsize`;
// pvresize (bool, default false) — runs `pvresize` on every PV in `pvs`
// after creating/extending; force (bool, default false) — allows
// `vgremove -f` to remove a VG that still has logical volumes;
// reset_vg_uuid / reset_pv_uuid (bool, default false) — `vgchange -u`/
// `pvchange -u` respectively; NOT idempotent, always reported changed
// when true (matching real lvg's own documented behavior); state
// (absent|present|active|inactive, default "present") — active/inactive
// imply present and additionally run `vgchange -a y`/`vgchange -a n`.
//
// VG existence and membership are read via `vgs --noheadings -o
// vg_name,pv_name --separator , <vg>` (RC != 0 means the VG doesn't
// exist yet). PV-is-already-a-PV is checked via `pvs --noheadings -o
// pv_name <dev>`.
//
// Simplifications vs real lvg: `pesize` is NOT verified against real
// lvg's own "does not modify PE size for an already-present volume
// group" note beyond simply never passing --physicalextentsize on an
// extend/reduce of an existing VG (matching that note); no `vg_uuid`
// alias lookup (only `vg` by name is supported, matching this port's
// standing "canonical name only" convention — see mount.go's own doc
// comment); no snapshot/clustering-related vgcreate flags beyond what a
// caller passes through `vg_options`.
func moduleLvg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	vg, err := requireString(args, "vg")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	force := argBool(args, "force", false)

	switch state {
	case "absent":
		exists, _, err := lvgVGInfo(ctx, conn, vg)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(vg + " already absent"), nil
		}
		cmd := "vgremove"
		if force {
			cmd += " -f"
		}
		cmd += " " + shellQuote(vg)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(vg + " removed"), nil

	case "present", "active", "inactive":
		pvs := argStringList(args, "pvs")
		pvOptions := argString(args, "pv_options", "")
		vgOptions := argString(args, "vg_options", "")
		pesize := argString(args, "pesize", "4")
		removeExtra := argBool(args, "remove_extra_pvs", true)
		pvresize := argBool(args, "pvresize", false)
		resetVG := argBool(args, "reset_vg_uuid", false)
		resetPV := argBool(args, "reset_pv_uuid", false)

		exists, curPVs, err := lvgVGInfo(ctx, conn, vg)
		if err != nil {
			return Result{}, err
		}
		changed := false

		if !exists {
			if len(pvs) == 0 {
				return Result{}, errArg("lvg: pvs is required to create volume group %q", vg)
			}
			for _, pv := range pvs {
				if err := lvgEnsurePV(ctx, conn, pv, pvOptions); err != nil {
					return Result{}, err
				}
			}
			cmd := "vgcreate -s " + shellQuote(pesize)
			if vgOptions != "" {
				cmd += " " + vgOptions
			}
			cmd += " " + shellQuote(vg) + " " + strings.Join(quotedList(pvs), " ")
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true
			curPVs = pvs
		} else if len(pvs) > 0 {
			var toAdd []string
			for _, pv := range pvs {
				if !containsStr(curPVs, pv) {
					toAdd = append(toAdd, pv)
				}
			}
			var toRemove []string
			if removeExtra {
				for _, pv := range curPVs {
					if !containsStr(pvs, pv) {
						toRemove = append(toRemove, pv)
					}
				}
			}
			for _, pv := range toAdd {
				if err := lvgEnsurePV(ctx, conn, pv, pvOptions); err != nil {
					return Result{}, err
				}
			}
			if len(toAdd) > 0 {
				cmd := "vgextend " + shellQuote(vg) + " " + strings.Join(quotedList(toAdd), " ")
				if _, err := run(ctx, conn, cmd); err != nil {
					return Result{}, err
				}
				changed = true
			}
			if len(toRemove) > 0 {
				cmd := "vgreduce " + shellQuote(vg) + " " + strings.Join(quotedList(toRemove), " ")
				if _, err := run(ctx, conn, cmd); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}

		if pvresize {
			for _, pv := range pvs {
				if _, err := run(ctx, conn, "pvresize "+shellQuote(pv)); err != nil {
					return Result{}, err
				}
			}
		}

		if resetVG {
			if _, err := run(ctx, conn, "vgchange -u "+shellQuote(vg)); err != nil {
				return Result{}, err
			}
			changed = true
		}
		if resetPV {
			pvArgs := pvs
			if len(pvArgs) == 0 {
				pvArgs = curPVs
			}
			for _, pv := range pvArgs {
				if _, err := run(ctx, conn, "pvchange -u "+shellQuote(pv)); err != nil {
					return Result{}, err
				}
			}
			changed = true
		}

		if state == "active" || state == "inactive" {
			flag := "y"
			if state == "inactive" {
				flag = "n"
			}
			if _, err := run(ctx, conn, "vgchange -a "+flag+" "+shellQuote(vg)); err != nil {
				return Result{}, err
			}
			// vgchange -a is idempotent in real LVM (no-op exit 0 when
			// already in that state), but this port has no portable way
			// to detect "already active" without parsing `vgs -o
			// vg_attr`; matching real lvg's own conservative choice,
			// this port does not attempt that here and instead leaves
			// active/inactive's changed-ness to whatever pvs/vg changes
			// already happened above, plus the state-toggle command
			// itself always being issued (harmless — LVM makes it a
			// no-op on the target).
		}

		if changed {
			return Changed(vg + " updated"), nil
		}
		return Ok(vg + " already up to date"), nil

	default:
		return Result{}, errArg("lvg: state must be absent, present, active, or inactive, got %q", state)
	}
}

// lvgVGInfo reports whether vg exists and, if so, its current PV list.
func lvgVGInfo(ctx context.Context, conn remoteexec.Connection, vg string) (exists bool, pvs []string, err error) {
	res, err := runStatus(ctx, conn, "vgs --noheadings -o pv_name "+shellQuote(vg)+" 2>/dev/null")
	if err != nil {
		return false, nil, err
	}
	if res.RC != 0 {
		return false, nil, nil
	}
	for _, line := range splitLines(res.Stdout) {
		line = strings.TrimSpace(line)
		if line != "" {
			pvs = append(pvs, line)
		}
	}
	return true, pvs, nil
}

// lvgEnsurePV runs `pvcreate` on pv unless it's already an LVM physical
// volume.
func lvgEnsurePV(ctx context.Context, conn remoteexec.Connection, pv, pvOptions string) error {
	res, err := runStatus(ctx, conn, "pvs --noheadings -o pv_name "+shellQuote(pv)+" 2>/dev/null")
	if err != nil {
		return err
	}
	if res.RC == 0 {
		return nil
	}
	cmd := "pvcreate"
	if pvOptions != "" {
		cmd += " " + pvOptions
	}
	cmd += " " + shellQuote(pv)
	_, err = run(ctx, conn, cmd)
	return err
}

func quotedList(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = shellQuote(s)
	}
	return out
}
