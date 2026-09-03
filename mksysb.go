package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMksysb implements Ansible's `mksysb` module (community.general):
// generates an AIX `rootvg` system backup image via `mksysb`.
//
// Args: name (string, required) — the image file's base name; storage_path
// (string, required) — an existing directory the image `<storage_path>/<name>`
// is written into; create_map_files (bool, default false) — `-m`; use_snapshot
// (bool, default false) — `-T`; exclude_files (bool, default false) — `-e`
// (excludes files listed in `/etc/rootvg.exclude`); exclude_wpar_files (bool,
// default false) — `-G`; software_packing (bool, default false) — `-p` is
// passed when this is FALSE (i.e. by DEFAULT), not when true — matching real
// mksysb.py's own `cmd_runner_fmt.as_bool_not("-p")`, whose flag is emitted on
// a false value; extended_attrs (bool, default true) — `-a`; backup_crypt_files
// (bool, default true) — `-Z` is passed when this is FALSE (i.e. NOT by
// default), matching real mksysb.py's own `as_bool_not("-Z")` the same way;
// backup_dmapi_fs (bool, default true) — `-A`; new_image_data (bool, default
// true) — `-i`.
//
// storage_path must already exist as a directory on the target (`test -d`);
// real mksysb.py checks this too (`os.path.isdir`, evaluated on the target
// since real Ansible modules execute there) and fails before running mksysb at
// all if not. mksysb itself is always run — this module has no way to check
// "is a backup with this name already up to date" (mksysb has no such notion),
// so, matching real mksysb.py's own `check_mode_skip=True` combined with always
// setting `self.changed = True`, every successful run reports Changed.
func moduleMksysb(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	storagePath, err := requireString(args, "storage_path")
	if err != nil {
		return Result{}, err
	}

	createMapFiles := argBool(args, "create_map_files", false)
	useSnapshot := argBool(args, "use_snapshot", false)
	excludeFiles := argBool(args, "exclude_files", false)
	excludeWparFiles := argBool(args, "exclude_wpar_files", false)
	softwarePacking := argBool(args, "software_packing", false)
	extendedAttrs := argBool(args, "extended_attrs", true)
	backupCryptFiles := argBool(args, "backup_crypt_files", true)
	backupDmapiFs := argBool(args, "backup_dmapi_fs", true)
	newImageData := argBool(args, "new_image_data", true)

	dirCheck, err := runStatus(ctx, conn, "test -d "+shellQuote(storagePath))
	if err != nil {
		return Result{}, err
	}
	if dirCheck.RC != 0 {
		return Fail(fmt.Sprintf("Storage path %s is not valid.", storagePath)), nil
	}

	var b strings.Builder
	b.WriteString("mksysb -X")
	if createMapFiles {
		b.WriteString(" -m")
	}
	if useSnapshot {
		b.WriteString(" -T")
	}
	if excludeFiles {
		b.WriteString(" -e")
	}
	if excludeWparFiles {
		b.WriteString(" -G")
	}
	if !softwarePacking {
		b.WriteString(" -p")
	}
	if extendedAttrs {
		b.WriteString(" -a")
	}
	if !backupCryptFiles {
		b.WriteString(" -Z")
	}
	if backupDmapiFs {
		b.WriteString(" -A")
	}
	if newImageData {
		b.WriteString(" -i")
	}
	b.WriteString(" " + shellQuote(storagePath+"/"+name))

	res, err := runStatus(ctx, conn, b.String())
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("mksysb failed: " + strings.TrimSpace(res.Stdout)), nil
	}
	return Changed(fmt.Sprintf("mksysb image %s/%s created.", storagePath, name)), nil
}
