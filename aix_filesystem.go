package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAixFilesystem implements (a subset of) Ansible's
// `aix_filesystem` module (community.general, deprecated upstream in
// favor of `ibm.power_aix.filesystem`): creates, removes, mounts,
// unmounts, or resizes an AIX LVM or NFS file system registered in
// `/etc/filesystems`, via `crfs`/`chfs`/`rmfs`/`mount`/`unmount`/
// `mknfsmnt`/`lsfs`/`showmount`.
//
// Args: filesystem (string, required) — the mount point; state
// (absent|mounted|present|unmounted, default "present"); device
// (string, optional) — an existing LV name, or (with nfs_server) the
// remote export path; vg (string, optional) — required (with no
// device) to create a fresh LV-backed file system; nfs_server
// (string, optional) — creates an NFS mount instead of an LVM one
// (requires device); size (string, optional) — 512-byte blocks unless
// suffixed M/G, or +/- prefixed for a relative resize; passed straight
// through to `crfs -a size=`/`chfs -a size=`, matching real
// aix_filesystem's own documented syntax; fs_type (string, default
// "jfs2"); permissions (rw|ro, default "rw"); mount_group (string,
// optional); auto_mount (bool, default true); account_subsystem
// (bool, default false); rm_mount_point (bool, default false) —
// `rmfs -r`; attributes ([]string, default ["agblksize=4096",
// "isnapshot=no"]) — each appended as its own `-a` to `crfs`.
//
// Whether `filesystem` is currently MOUNTED is checked via `mount |
// grep -qFw -- filesystem` — an approximation of real
// aix_filesystem.py's own os.path.ismount() (which compares device
// IDs and is therefore immune to a path merely appearing as a
// substring/whole-word elsewhere in `mount`'s own output); this port
// runs on the control node, not the target, so it has no local stat()
// to call and must infer mount state from `mount`'s own text output
// instead — a documented, narrow gap. Whether it EXISTS (regardless of
// mount state) is checked via `lsfs -l filesystem` (rc==1 with "No
// record matching" in stderr means absent).
//
// Two deviations from real aix_filesystem.py's own command
// construction, both DELIBERATE: (1) its remove_fs() always appends a
// literal "-r" to its `rmfs` argv, THEN separately appends
// rm_mount_point's own "-r"-or-"" value right after it — meaning real
// aix_filesystem removes the mount point directory unconditionally,
// contradicting its own documented "rm_mount_point: removes the mount
// point directory... when used with state=absent" (implying opt-in);
// this port passes `-r` only when rm_mount_point=true, matching the
// documented behavior. (2) its create_fs() builds the NFS `mknfsmnt`
// argv as `["-f", filesystem, device, "-h", nfs_server, ...]` — device
// is inserted as a bare positional argument with no preceding `-d`
// flag, which real mknfsmnt(1) syntax requires; this port passes `-d
// device` explicitly.
//
// Real aix_filesystem.py also has a documented-but-silent gap this
// port improves on rather than replicates: when nfs_server AND device
// are both given but showmount doesn't list device as exported by
// nfs_server, real aix_filesystem falls through all three of its
// top-level `if` blocks and exits with changed=false and an EMPTY
// message; this port returns a clear "not exported" message instead
// in that case.
func moduleAixFilesystem(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	filesystem, err := requireString(args, "filesystem")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	device := argString(args, "device", "")
	vg := argString(args, "vg", "")
	nfsServer := argString(args, "nfs_server", "")
	size := argString(args, "size", "")
	fsType := argString(args, "fs_type", "jfs2")
	permissions := argString(args, "permissions", "rw")
	mountGroup := argString(args, "mount_group", "")
	autoMount := argBool(args, "auto_mount", true)
	accountSubsystem := argBool(args, "account_subsystem", false)
	rmMountPoint := argBool(args, "rm_mount_point", false)
	attributes := argStringList(args, "attributes")
	if len(attributes) == 0 {
		attributes = []string{"agblksize=4096", "isnapshot=no"}
	}

	switch state {
	case "present":
		mounted, err := aixFilesystemMounted(ctx, conn, filesystem)
		if err != nil {
			return Result{}, err
		}
		exists, err := aixFilesystemExists(ctx, conn, filesystem)
		if err != nil {
			return Result{}, err
		}
		if mounted || exists {
			if size != "" {
				return aixFilesystemResize(ctx, conn, filesystem, size)
			}
			return Ok(fmt.Sprintf("File system %s already exists.", filesystem)), nil
		}
		if nfsServer != "" {
			if device == "" {
				return Fail(`Parameter "device" is required when "nfs_server" is defined.`), nil
			}
			exported, err := aixFilesystemNFSExported(ctx, conn, nfsServer, device)
			if err != nil {
				return Result{}, err
			}
			if !exported {
				return Ok(fmt.Sprintf("%s is not exported by %s; no NFS file system created.", device, nfsServer)), nil
			}
			return aixFilesystemCreateNFS(ctx, conn, filesystem, device, nfsServer, permissions, autoMount)
		}
		if device == "" && vg == "" {
			return Fail(`Required parameter "device" and/or "vg" is missing for filesystem creation.`), nil
		}
		return aixFilesystemCreateLVM(ctx, conn, fsType, filesystem, vg, device, size, mountGroup, autoMount, accountSubsystem, permissions, attributes)

	case "absent":
		mounted, err := aixFilesystemMounted(ctx, conn, filesystem)
		if err != nil {
			return Result{}, err
		}
		if mounted {
			return Ok(fmt.Sprintf("File system %s mounted.", filesystem)), nil
		}
		exists, err := aixFilesystemExists(ctx, conn, filesystem)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(fmt.Sprintf("File system %s does not exist.", filesystem)), nil
		}
		return aixFilesystemRemove(ctx, conn, filesystem, rmMountPoint)

	case "mounted":
		mounted, err := aixFilesystemMounted(ctx, conn, filesystem)
		if err != nil {
			return Result{}, err
		}
		if mounted {
			return Ok(fmt.Sprintf("File system %s already mounted.", filesystem)), nil
		}
		if _, err := run(ctx, conn, "mount "+shellQuote(filesystem)); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("File system %s mounted.", filesystem)), nil

	case "unmounted":
		mounted, err := aixFilesystemMounted(ctx, conn, filesystem)
		if err != nil {
			return Result{}, err
		}
		if !mounted {
			return Ok(fmt.Sprintf("File system %s already unmounted.", filesystem)), nil
		}
		if _, err := run(ctx, conn, "unmount "+shellQuote(filesystem)); err != nil {
			return Result{}, err
		}
		return Changed(fmt.Sprintf("File system %s unmounted.", filesystem)), nil

	default:
		return Result{}, errArg("aix_filesystem: state must be absent, mounted, present, or unmounted, got %q", state)
	}
}

func aixFilesystemMounted(ctx context.Context, conn remoteexec.Connection, filesystem string) (bool, error) {
	res, err := runStatus(ctx, conn, "mount | grep -qFw -- "+shellQuote(filesystem))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

func aixFilesystemExists(ctx context.Context, conn remoteexec.Connection, filesystem string) (bool, error) {
	res, err := runStatus(ctx, conn, "lsfs -l "+shellQuote(filesystem))
	if err != nil {
		return false, err
	}
	if res.RC == 0 {
		return true, nil
	}
	if res.RC == 1 && strings.Contains(res.Stderr, "No record matching") {
		return false, nil
	}
	return false, fmt.Errorf("aix_filesystem: lsfs failed: %s", strings.TrimSpace(res.Stderr))
}

func aixFilesystemNFSExported(ctx context.Context, conn remoteexec.Connection, nfsServer, device string) (bool, error) {
	res, err := runStatus(ctx, conn, "showmount -a "+shellQuote(nfsServer))
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("aix_filesystem: showmount failed: %s", strings.TrimSpace(res.Stderr))
	}
	for _, line := range splitLines(res.Stdout) {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && parts[1] == device {
			return true, nil
		}
	}
	return false, nil
}

func aixFilesystemResize(ctx context.Context, conn remoteexec.Connection, filesystem, size string) (Result, error) {
	res, err := runStatus(ctx, conn, "chfs -a size="+shellQuote(size)+" "+shellQuote(filesystem))
	if err != nil {
		return Result{}, err
	}
	if res.RC == 28 {
		return Ok(strings.TrimSpace(res.Stdout)), nil
	}
	if res.RC != 0 {
		if strings.Contains(res.Stderr, "Maximum allocation for logical") {
			return Ok(strings.TrimSpace(res.Stderr)), nil
		}
		return Fail("aix_filesystem: chfs failed: " + strings.TrimSpace(res.Stderr)), nil
	}
	if strings.Contains(res.Stdout, "The filesystem size is already") {
		return Ok(strings.TrimSpace(res.Stdout)), nil
	}
	return Changed(strings.TrimSpace(res.Stdout)), nil
}

func aixFilesystemRemove(ctx context.Context, conn remoteexec.Connection, filesystem string, rmMountPoint bool) (Result, error) {
	cmd := "rmfs"
	if rmMountPoint {
		cmd += " -r"
	}
	cmd += " " + shellQuote(filesystem)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("aix_filesystem: rmfs failed: " + strings.TrimSpace(res.Stderr)), nil
	}
	msg := strings.TrimSpace(res.Stdout)
	if msg == "" {
		msg = fmt.Sprintf("File system %s removed.", filesystem)
	}
	return Changed(msg), nil
}

func aixFilesystemCreateNFS(ctx context.Context, conn remoteexec.Connection, filesystem, device, nfsServer, permissions string, autoMount bool) (Result, error) {
	flag := "-a"
	if autoMount {
		flag = "-A"
	}
	cmd := "mknfsmnt -f " + shellQuote(filesystem) + " -d " + shellQuote(device) + " -h " + shellQuote(nfsServer) +
		" -t " + shellQuote(permissions) + " " + flag + " -w bg"
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("aix_filesystem: mknfsmnt failed: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Changed(fmt.Sprintf("NFS file system %s created.", filesystem)), nil
}

func aixFilesystemCreateLVM(ctx context.Context, conn remoteexec.Connection, fsType, filesystem, vg, device, size, mountGroup string, autoMount, accountSubsystem bool, permissions string, attributes []string) (Result, error) {
	if vg != "" {
		vgState, err := aixVGState(ctx, conn, vg)
		if err != nil {
			return Result{}, err
		}
		if vgState == nil {
			return Ok(fmt.Sprintf("Volume group %s does not exist.", vg)), nil
		}
		if !*vgState {
			return Ok(fmt.Sprintf("Volume group %s is in varyoff state.", vg)), nil
		}
	}

	cmd := "crfs -v " + shellQuote(fsType)
	if vg != "" {
		cmd += " -g " + shellQuote(vg)
	}
	if device != "" {
		cmd += " -d " + shellQuote(device)
	}
	cmd += " -m " + shellQuote(filesystem)
	if mountGroup != "" {
		cmd += " -u " + shellQuote(mountGroup)
	}
	if autoMount {
		cmd += " -A yes"
	} else {
		cmd += " -A no"
	}
	if accountSubsystem {
		cmd += " -t yes"
	} else {
		cmd += " -t no"
	}
	cmd += " -p " + permissions
	if size != "" {
		cmd += " -a size=" + shellQuote(size)
	}
	for _, a := range attributes {
		cmd += " -a " + shellQuote(a)
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC == 10 {
		return Ok(fmt.Sprintf("Using an existing previously defined logical volume, volume group needs to be empty. %s", strings.TrimSpace(res.Stderr))), nil
	}
	if res.RC != 0 {
		return Fail("aix_filesystem: crfs failed: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Changed(strings.TrimSpace(res.Stdout)), nil
}
