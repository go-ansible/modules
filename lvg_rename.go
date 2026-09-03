package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLvgRename implements Ansible's `lvg_rename` module: renames an
// LVM volume group via `vgrename`.
//
// Args: vg (string) — the source VG's current name; vg_new (string,
// required) — the desired name. Real lvg_rename also accepts a
// `vg_uuid` alternative to `vg` (renaming by UUID instead of current
// name); this port does not implement that lookup (only vg-by-name is
// supported, matching this port's standing "canonical identifier only"
// convention — see mount.go's own doc comment) — a caller wanting to
// rename by UUID gets a clear argument error rather than a silent
// no-op.
//
// Idempotency: if a VG named vg_new already exists AND no VG named vg
// exists, this is treated as already-renamed (Ok, unchanged) — matching
// real lvg_rename's own idempotent re-run behavior. If neither vg nor
// vg_new exists, this fails cleanly (nothing to rename). If both exist,
// this also fails cleanly, since running vgrename would either error
// out (name collision) or silently rename the wrong VG depending on
// real vgrename's own particulars — this port does not guess.
func moduleLvgRename(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	vg, err := requireString(args, "vg")
	if err != nil {
		return Result{}, err
	}
	vgNew, err := requireString(args, "vg_new")
	if err != nil {
		return Result{}, err
	}

	oldExists, _, err := lvgVGInfo(ctx, conn, vg)
	if err != nil {
		return Result{}, err
	}
	newExists, _, err := lvgVGInfo(ctx, conn, vgNew)
	if err != nil {
		return Result{}, err
	}

	switch {
	case oldExists && newExists:
		return Fail("lvg_rename: both " + vg + " and " + vgNew + " exist; refusing to guess which vgrename would do"), nil
	case !oldExists && newExists:
		return Ok(vg + " already renamed to " + vgNew), nil
	case !oldExists && !newExists:
		return Fail("lvg_rename: neither " + vg + " nor " + vgNew + " exists"), nil
	}

	if _, err := run(ctx, conn, "vgrename "+shellQuote(vg)+" "+shellQuote(vgNew)); err != nil {
		return Result{}, err
	}
	return Changed(vg + " renamed to " + vgNew), nil
}
