package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAixLvol implements (a subset of) Ansible's `aix_lvol` module
// (community.general, deprecated upstream in favor of
// `ibm.power_aix.lvol`): creates, removes, or resizes an AIX logical
// volume via `mklv`/`extendlv`/`rmlv`/`chlv`/`lsvg`/`lslv`.
//
// Args: vg (string, required); lv (string, required); lv_type
// (string, default "jfs2") — `mklv -t`; size (string, optional but
// required to create) — MUST end in M/G/T (bare-number sizes, unlike
// lvol.go's own broader parser, are rejected — matching real
// aix_lvol.py's own convert_size(), which does not accept a bare
// number); opts (string, default "") — inserted verbatim into `mklv`
// (see filesystem.go's doc comment for this port's house convention on
// free-form opts arguments); copies (int, default 1) — `mklv -c`;
// policy (maximum|minimum, default "maximum") — `-e x`/`-e m`; pvs
// ([]string, default []) — physical volumes a NEW plain LV is
// constrained to; state (present|absent, default "present").
//
// A requested size is converted to megabytes and rounded UP to the
// volume group's own PP size (read from `lsvg <vg>`'s "PP SIZE:" and
// "FREE PP...(...)" fields — the latter is already reported in
// megabytes by real `lsvg`, not PP count, matching real aix_lvol.py's
// own use of it). Creating a new LV fails if the rounded size exceeds
// the VG's free space. An existing LV's size/policy/owning-VG are read
// from `lslv <lv>`'s "LOGICAL VOLUME: name VOLUME GROUP: vg", "LPs:
// N ... PPs", and "INTER-POLICY: policy" fields. Extending is done via
// `extendlv <lv> <delta>M`; SHRINKING an existing LV is refused
// (Fail), matching real aix_lvol's own documented behavior (there is
// no `force`/`shrink` escape hatch here, unlike lvol.go's own LVM
// equivalent). A policy mismatch on an existing LV is fixed via `chlv
// -e`, and — matching real aix_lvol.py's own control flow exactly — is
// applied and reported as the WHOLE outcome of the call, before any
// size check even runs (a real request that changes both policy and
// size in one task only ever reports the policy change; a second run
// is needed to pick up the resize, since after `chlv` this port also
// returns immediately rather than falling through to the size logic,
// matching upstream's own early exit_json()).
//
// The rounding step is an approximation of real aix_lvol.py's own
// Python round() (banker's rounding) using round-half-up instead;
// this can disagree by one PP boundary in the (rare) exact-half case,
// a documented, narrow deviation.
func moduleAixLvol(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	vg, err := requireString(args, "vg")
	if err != nil {
		return Result{}, err
	}
	lv, err := requireString(args, "lv")
	if err != nil {
		return Result{}, err
	}
	lvType := argString(args, "lv_type", "jfs2")
	size := argString(args, "size", "")
	opts := argString(args, "opts", "")
	copies := argInt(args, "copies", 1)
	state := argString(args, "state", "present")
	policy := argString(args, "policy", "maximum")
	pvs := argStringList(args, "pvs")
	if state != "present" && state != "absent" {
		return Result{}, errArg("aix_lvol: state must be present or absent, got %q", state)
	}

	lvPolicyFlag := "x"
	if policy == "minimum" {
		lvPolicyFlag = "m"
	}

	ppSizeMB, freeMB, vgExists, err := aixVGInfo(ctx, conn, vg)
	if err != nil {
		return Result{}, err
	}
	if !vgExists {
		if state == "absent" {
			return Ok(fmt.Sprintf("Volume group %s does not exist.", vg)), nil
		}
		return Fail(fmt.Sprintf("Volume group %s does not exist.", vg)), nil
	}

	var lvSizeMB int
	if size != "" {
		raw, ok := aixLVConvertSizeMB(size)
		if !ok {
			return Fail("No valid size unit specified."), nil
		}
		lvSizeMB = aixRoundPPSize(raw, ppSizeMB)
	}

	curSizeMB, curPolicy, curVG, lvExists, err := aixLVInfo(ctx, conn, lv)
	if err != nil {
		return Result{}, err
	}
	if !lvExists && state == "absent" {
		return Ok(fmt.Sprintf("Logical Volume %s does not exist.", lv)), nil
	}

	if !lvExists {
		if state != "present" {
			return Ok(fmt.Sprintf("Logical Volume %s does not exist.", lv)), nil
		}
		if size == "" {
			return Fail("No size given."), nil
		}
		if lvSizeMB > freeMB {
			return Fail(fmt.Sprintf("Not enough free space in volume group %s: %d MB free.", vg, freeMB)), nil
		}
		cmd := "mklv -t " + shellQuote(lvType) + " -y " + shellQuote(lv) + " -c " + strconv.Itoa(copies) + " -e " + lvPolicyFlag
		if opts != "" {
			cmd += " " + opts
		}
		cmd += " " + shellQuote(vg) + " " + strconv.Itoa(lvSizeMB) + "M"
		for _, pv := range pvs {
			cmd += " " + shellQuote(pv)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("Logical volume %s created.", lv)), nil
	}

	if state == "absent" {
		if _, err := run(ctx, conn, "rmlv -f "+shellQuote(lv)); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("Logical volume %s deleted.", lv)), nil
	}

	if curPolicy != policy {
		if _, err := run(ctx, conn, "chlv -e "+lvPolicyFlag+" "+shellQuote(lv)); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("Logical volume %s policy changed: %s.", lv, policy)), nil
	}

	if curVG != vg {
		return Fail(fmt.Sprintf("Logical volume %s already exist in volume group %s", lv, curVG)), nil
	}

	if size == "" {
		return Ok(fmt.Sprintf("Logical volume %s already exist.", lv)), nil
	}

	switch {
	case lvSizeMB > curSizeMB:
		delta := lvSizeMB - curSizeMB
		if _, err := run(ctx, conn, "extendlv "+shellQuote(lv)+" "+strconv.Itoa(delta)+"M"); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("Logical volume %s size extended to %dMB.", lv, lvSizeMB)), nil
	case lvSizeMB < curSizeMB:
		return Fail(fmt.Sprintf("No shrinking of Logical Volume %s permitted. Current size: %d MB", lv, curSizeMB)), nil
	default:
		return Ok(fmt.Sprintf("Logical volume %s size is already %dMB.", lv, lvSizeMB)), nil
	}
}

var (
	aixVGPPSizeRE = regexp.MustCompile(`PP SIZE:\s+(\d+)`)
	aixVGFreeRE   = regexp.MustCompile(`FREE PP.*\((\d+)`)
	aixLVHeaderRE = regexp.MustCompile(`LOGICAL VOLUME:\s+(\S+)\s+VOLUME GROUP:\s+(\S+)`)
	aixLVSizeRE   = regexp.MustCompile(`LPs:\s+(\d+).*PPs`)
	aixLVPolicyRE = regexp.MustCompile(`INTER-POLICY:\s+(\S+)`)
)

// aixVGInfo reads a volume group's PP size and free space (both in
// megabytes) from `lsvg <vg>`.
func aixVGInfo(ctx context.Context, conn remoteexec.Connection, vg string) (ppSizeMB, freeMB int, exists bool, err error) {
	res, err := runStatus(ctx, conn, "lsvg "+shellQuote(vg))
	if err != nil {
		return 0, 0, false, err
	}
	if res.RC != 0 {
		return 0, 0, false, nil
	}
	ppM := aixVGPPSizeRE.FindStringSubmatch(res.Stdout)
	freeM := aixVGFreeRE.FindStringSubmatch(res.Stdout)
	if ppM == nil || freeM == nil {
		return 0, 0, true, fmt.Errorf("aix_lvol: could not parse lsvg output for %s", vg)
	}
	ppSizeMB, _ = strconv.Atoi(ppM[1])
	freeMB, _ = strconv.Atoi(freeM[1])
	return ppSizeMB, freeMB, true, nil
}

// aixLVInfo reads a logical volume's size (megabytes), interphysical
// allocation policy, and owning volume group from `lslv <lv>`.
func aixLVInfo(ctx context.Context, conn remoteexec.Connection, lv string) (sizeMB int, policy, vg string, exists bool, err error) {
	res, err := runStatus(ctx, conn, "lslv "+shellQuote(lv))
	if err != nil {
		return 0, "", "", false, err
	}
	if res.RC != 0 {
		return 0, "", "", false, nil
	}
	lpsM := aixLVSizeRE.FindStringSubmatch(res.Stdout)
	polM := aixLVPolicyRE.FindStringSubmatch(res.Stdout)
	ppSizeM := aixVGPPSizeRE.FindStringSubmatch(res.Stdout)
	hdrM := aixLVHeaderRE.FindStringSubmatch(res.Stdout)
	if lpsM == nil || polM == nil || ppSizeM == nil || hdrM == nil {
		return 0, "", "", true, fmt.Errorf("aix_lvol: could not parse lslv output for %s", lv)
	}
	lps, _ := strconv.Atoi(lpsM[1])
	ppSize, _ := strconv.Atoi(ppSizeM[1])
	return lps * ppSize, polM[1], hdrM[2], true, nil
}

// aixLVConvertSizeMB converts a real aix_lvol `size` value (a number
// followed by M, G, or T — case-insensitive; no bare-number form) to
// megabytes.
func aixLVConvertSizeMB(size string) (int, bool) {
	if size == "" {
		return 0, false
	}
	unit := strings.ToUpper(size[len(size)-1:])
	mult, ok := map[string]int{"M": 1, "G": 1024, "T": 1024 * 1024}[unit]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(size[:len(size)-1])
	if err != nil {
		return 0, false
	}
	return n * mult, true
}

// aixRoundPPSize rounds x up to the nearest multiple of base, using
// round-half-up for the halfway point (see the module doc comment for
// how this differs from real aix_lvol.py's own Python round()).
func aixRoundPPSize(x, base int) int {
	if base <= 0 {
		return x
	}
	q := x / base
	r := x % base
	newSize := q * base
	if r*2 >= base {
		newSize += base
	}
	if newSize < x {
		newSize += base
	}
	return newSize
}
