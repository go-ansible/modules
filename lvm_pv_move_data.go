package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLvmPvMoveData implements Ansible's `lvm_pv_move_data` module:
// moves allocated LVM extents from one physical volume to another via
// `pvmove`.
//
// Args: source (string, required); destination (string, required);
// atomic (bool, default true) — `pvmove --atomic`; auto_answer (bool,
// default false) — `pvmove -y`; autobackup (bool, default true) — real
// pvmove's own default is already to back up metadata, matching this
// argument's default; passed as `--autobackup y`/`--autobackup n`.
//
// This port does not verify ahead of time that source/destination are
// both PVs in the same volume group, or that destination has enough
// free space — real lvm_pv_move_data's own requirements note both, but
// this port defers entirely to `pvmove` itself to enforce them and
// surface its own error if they're not met, rather than duplicating
// that validation with a separate `pvs`/`vgs` probe.
//
// Idempotency: this port checks whether source currently has any
// allocated extents via `pvs --noheadings -o pv_name -O -pv_used
// source` filtered by pv_used != 0 (specifically `pvs --noheadings -o
// pv_used --units b --nosuffix source`); if pv_used is "0" (or the
// field can't be read, e.g. because source isn't a PV in a VG at all),
// this reports Ok/unchanged with the message "no allocated extents to
// move" — matching real lvm_pv_move_data's own documented return value
// sample of that exact phrase — without invoking pvmove at all, since
// pvmove has nothing to do in that case.
func moduleLvmPvMoveData(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	source, err := requireString(args, "source")
	if err != nil {
		return Result{}, err
	}
	destination, err := requireString(args, "destination")
	if err != nil {
		return Result{}, err
	}
	atomic := argBool(args, "atomic", true)
	autoAnswer := argBool(args, "auto_answer", false)
	autobackup := argBool(args, "autobackup", true)

	used, err := lvmPvUsedBytes(ctx, conn, source)
	if err != nil {
		return Result{}, err
	}
	if used == 0 {
		return Ok("no allocated extents to move").WithExtra("actions", []string{"no allocated extents to move"}), nil
	}

	cmd := "pvmove"
	if atomic {
		cmd += " --atomic"
	}
	if autoAnswer {
		cmd += " -y"
	}
	if autobackup {
		cmd += " --autobackup y"
	} else {
		cmd += " --autobackup n"
	}
	cmd += " " + shellQuote(source) + " " + shellQuote(destination)

	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	action := "moved data from " + source + " to " + destination
	return Changed(action).WithExtra("actions", []string{action}), nil
}

// lvmPvUsedBytes reads source's currently-allocated (used) extent size
// in bytes via `pvs`, returning 0 if the field can't be read (e.g.
// source isn't a PV at all) — treated by the caller the same as
// "nothing to move".
func lvmPvUsedBytes(ctx context.Context, conn remoteexec.Connection, pv string) (int64, error) {
	res, err := runStatus(ctx, conn, "pvs --noheadings -o pv_used --units b --nosuffix "+shellQuote(pv)+" 2>/dev/null")
	if err != nil {
		return 0, err
	}
	if res.RC != 0 {
		return 0, nil
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" || out == "0" {
		return 0, nil
	}
	n, ok := parseLVSize(out + "b")
	if !ok {
		return 0, nil
	}
	return n, nil
}
