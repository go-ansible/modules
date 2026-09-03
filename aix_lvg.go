package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAixLvg implements (a subset of) Ansible's `aix_lvg` module
// (community.general, deprecated upstream in favor of
// `ibm.power_aix.lvg`): creates, removes, extends/reduces, or varies
// on/off an AIX LVM volume group via `mkvg`/`extendvg`/`reducevg`/
// `varyonvg`/`varyoffvg`/`lsvg`/`lspv`.
//
// Args: vg (string, required); pvs ([]string) — physical volumes;
// required to create a VG, and (for state=present) any name in this
// list not already IN this vg is added via `extendvg`; pp_size (int,
// optional) — `mkvg -s <pp_size>` (megabytes; only applies at
// creation, matching real aix_lvg's own documented "does not modify PP
// size for an already present volume group"); vg_type (normal|big|
// scalable, default "normal") — `mkvg -B`/`-S` (no flag for normal);
// force (bool, default false) — `mkvg -f`; state (absent|present|
// varyoff|varyon, default "present").
//
// A PV's usability is checked via `lspv`'s third column (the owning
// VG name, or "None" when unused): a PV not listed by `lspv` at all is
// a hard failure; one already owned by a DIFFERENT vg is a hard
// failure; one owned by THIS vg is left alone (no-op for that PV);
// "None" means it's free to add. This port validates and folds in
// EVERY pv given this way — a deliberate deviation from real
// aix_lvg.py's own `_validate_pv()`, whose `for pv in pvs:` loop body
// unconditionally `return`s on its very first iteration, so it only
// ever validates (and short-circuits create_extend_vg's whole outcome
// on) pvs[0], silently ignoring the validity of any additional pvs in
// the list; replicating that would make a multi-pv `pvs: [hdisk1,
// hdisk2]` extend request behave inconsistently depending on which pv
// happens to be first, which this port judged not worth reproducing
// given real aix_lvg's own EXAMPLES document exactly that multi-pv
// shape.
//
// VG existence/varyon state is read via `lsvg -o` (varied-on VGs) and
// `lsvg` (all VGs) — exact line membership, not substring. state=
// absent with no `pvs` removes the VG entirely (`reducevg -df vg
// <every current pv, from 'lsvg -p vg'>`); with `pvs` given, only
// those pvs are reduced out.
func moduleAixLvg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	vg, err := requireString(args, "vg")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	force := argBool(args, "force", false)
	pvs := argStringList(args, "pvs")
	ppSize := argInt(args, "pp_size", 0)
	vgType := argString(args, "vg_type", "normal")

	vgState, err := aixVGState(ctx, conn, vg)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "present":
		if len(pvs) == 0 {
			return Fail("pvs is required to state 'present'."), nil
		}
		return aixLvgCreateExtend(ctx, conn, vg, pvs, ppSize, vgType, force, vgState)
	case "absent":
		return aixLvgReduce(ctx, conn, vg, pvs, vgState)
	case "varyon", "varyoff":
		return aixLvgVaryState(ctx, conn, vg, state, vgState)
	default:
		return Result{}, errArg("aix_lvg: state must be absent, present, varyoff, or varyon, got %q", state)
	}
}

// aixVGState reports a volume group's varyon state: nil means the VG
// does not exist, else a pointer to true (varyon) or false (varyoff).
func aixVGState(ctx context.Context, conn remoteexec.Connection, vg string) (*bool, error) {
	activeOut, err := run(ctx, conn, "lsvg -o")
	if err != nil {
		return nil, err
	}
	allOut, err := run(ctx, conn, "lsvg")
	if err != nil {
		return nil, err
	}
	active := splitLines(activeOut)
	all := splitLines(allOut)
	if containsStr(all, vg) && !containsStr(active, vg) {
		f := false
		return &f, nil
	}
	if containsStr(active, vg) {
		t := true
		return &t, nil
	}
	return nil, nil
}

// aixLvgPVStatus reports whether pv is known to `lspv` and, if so, its
// owning VG name ("None" if unused by any VG).
func aixLvgPVStatus(ctx context.Context, conn remoteexec.Connection, pv string) (exists bool, owner string, err error) {
	out, err := run(ctx, conn, "lspv")
	if err != nil {
		return false, "", err
	}
	for _, line := range splitLines(out) {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == pv {
			return true, fields[2], nil
		}
	}
	return false, "", nil
}

func aixLvgCreateExtend(ctx context.Context, conn remoteexec.Connection, vg string, pvs []string, ppSize int, vgType string, force bool, vgState *bool) (Result, error) {
	var newPVs []string
	for _, pv := range pvs {
		exists, owner, err := aixLvgPVStatus(ctx, conn, pv)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail(fmt.Sprintf("Physical volume '%s' does not exist.", pv)), nil
		}
		if owner == "None" {
			newPVs = append(newPVs, pv)
			continue
		}
		if owner != vg {
			return Fail(fmt.Sprintf("Physical volume '%s' is in use by another volume group '%s'.", pv, owner)), nil
		}
	}

	if vgState == nil {
		cmd := "mkvg"
		switch vgType {
		case "big":
			cmd += " -B"
		case "scalable":
			cmd += " -S"
		}
		if ppSize > 0 {
			cmd += " -s " + strconv.Itoa(ppSize)
		}
		if force {
			cmd += " -f"
		}
		cmd += " -y " + shellQuote(vg) + " " + strings.Join(quotedList(pvs), " ")
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("Volume group '%s' created.", vg)), nil
	}
	if !*vgState {
		return Fail(fmt.Sprintf("Volume group '%s' is in varyoff state.", vg)), nil
	}
	if len(newPVs) == 0 {
		return Ok(fmt.Sprintf("Volume group '%s' already contains all specified physical volumes.", vg)), nil
	}
	cmd := "extendvg " + shellQuote(vg) + " " + strings.Join(quotedList(newPVs), " ")
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(fmt.Sprintf("Volume group '%s' extended.", vg)), nil
}

func aixLvgReduce(ctx context.Context, conn remoteexec.Connection, vg string, pvs []string, vgState *bool) (Result, error) {
	if vgState == nil {
		return Ok(fmt.Sprintf("Volume group '%s' does not exist.", vg)), nil
	}
	if !*vgState {
		return Fail(fmt.Sprintf("Volume group '%s' is in varyoff state.", vg)), nil
	}

	toRemove := pvs
	msg := fmt.Sprintf("Physical volume(s) '%s' removed from Volume group '%s'.", strings.Join(pvs, " "), vg)
	if len(toRemove) == 0 {
		out, err := run(ctx, conn, "lsvg -p "+shellQuote(vg))
		if err != nil {
			return Result{}, err
		}
		lines := splitLines(out)
		if len(lines) > 2 {
			for _, l := range lines[2:] {
				fields := strings.Fields(l)
				if len(fields) > 0 {
					toRemove = append(toRemove, fields[0])
				}
			}
		}
		msg = fmt.Sprintf("Volume group '%s' removed.", vg)
	}
	if len(toRemove) == 0 {
		return Ok("No physical volumes to remove."), nil
	}
	cmd := "reducevg -df " + shellQuote(vg) + " " + strings.Join(quotedList(toRemove), " ")
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(msg), nil
}

func aixLvgVaryState(ctx context.Context, conn remoteexec.Connection, vg, state string, vgState *bool) (Result, error) {
	if vgState == nil {
		return Fail(fmt.Sprintf("Volume group '%s' does not exist.", vg)), nil
	}
	if state == "varyon" {
		if *vgState {
			return Ok(fmt.Sprintf("Volume group '%s' is in varyon state.", vg)), nil
		}
		if _, err := run(ctx, conn, "varyonvg "+shellQuote(vg)); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("Varyon volume group %s completed.", vg)), nil
	}
	if !*vgState {
		return Ok(fmt.Sprintf("Volume group '%s' is in varyoff state.", vg)), nil
	}
	if _, err := run(ctx, conn, "varyoffvg "+shellQuote(vg)); err != nil {
		return Result{}, err
	}
	return Changed(fmt.Sprintf("Varyoff volume group %s completed.", vg)), nil
}
