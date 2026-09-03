package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleBeadm implements (a subset of) Ansible's `beadm` module
// (community.general): creates, removes, activates, mounts, or
// unmounts a ZFS boot environment on FreeBSD/Solaris/illumos, via
// `beadm create`/`activate`/`destroy`/`mount`/`unmount`/`list`.
//
// Args: name (string, required, alias be) — the BE name (may include
// a "@snapshot" suffix, e.g. for state=present cloning); snapshot
// (string, optional) — `beadm create -e <snapshot>`; description
// (string, optional, Solarish only) — `beadm create -d`; options
// (string, optional, Solarish only) — `beadm create -o`; mountpoint
// (path, optional) — where state=mounted mounts the BE; state
// (absent|activated|mounted|present|unmounted, default "present");
// force (bool, default false) — `beadm unmount -f`.
//
// This port runs `uname -s` against the target to distinguish FreeBSD
// from Solarish behavior, matching real beadm.py's own `os.uname()[0]
// == "FreeBSD"` check — an extra round-trip real beadm.py doesn't need
// since it executes locally ON the target; this port instead reaches
// the target only through the Connection's Exec, so it must ask.
//
// A BE's presence/activation/mount state is read by parsing `beadm
// list -H` (Solarish: ";"-separated fields, activation in field[2]
// containing "R", mount point truthy in field[3]; FreeBSD:
// whitespace-separated fields, activation in field[1] containing "R",
// mounted when field[2] is neither "-" nor "/") — matching real
// beadm.py's own _find_be_by_name/is_activated/is_mounted parsing
// exactly, including its "@"-suffixed name lookup (matches against a
// name's last "/"-separated component on FreeBSD's snapshot listing).
// state=absent refuses to destroy a MOUNTED BE (matching real beadm.py
// exactly, on both platforms), and additionally refuses an ACTIVATED
// one on FreeBSD only (state=activated correspondingly refuses a
// MOUNTED BE, FreeBSD only) — real beadm.py's own documented
// platform-specific caveats, not gaps in this port.
func moduleBeadm(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", argString(args, "be", ""))
	if name == "" {
		return Result{}, errArg("beadm: missing required argument: name (or its alias be)")
	}
	snapshot := argString(args, "snapshot", "")
	description := argString(args, "description", "")
	options := argString(args, "options", "")
	mountpoint := argString(args, "mountpoint", "")
	state := argString(args, "state", "present")
	force := argBool(args, "force", false)

	isFreeBSD, err := beadmIsFreeBSD(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	listArgs := "list -H"
	if strings.Contains(name, "@") {
		listArgs += " -s"
	}
	listOut, err := run(ctx, conn, "beadm "+listArgs)
	if err != nil {
		return Result{}, err
	}
	fields, exists := beadmFindByName(listOut, name, isFreeBSD)
	activated := exists && beadmIsActivated(fields, isFreeBSD)
	mounted := exists && beadmIsMounted(fields, isFreeBSD)

	switch state {
	case "absent":
		if !exists {
			return Ok(name + " already absent"), nil
		}
		if mounted {
			return Fail("Unable to remove BE as it is mounted!"), nil
		}
		if isFreeBSD && activated {
			return Fail("Unable to remove active BE!"), nil
		}
		if _, err := run(ctx, conn, "beadm destroy -F "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " destroyed"), nil

	case "present":
		if exists {
			return Ok(name + " already exists"), nil
		}
		cmd := "beadm create"
		if snapshot != "" {
			cmd += " -e " + shellQuote(snapshot)
		}
		if !isFreeBSD {
			if description != "" {
				cmd += " -d " + shellQuote(description)
			}
			if options != "" {
				cmd += " -o " + options
			}
		}
		cmd += " " + shellQuote(name)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(name + " created"), nil

	case "activated":
		if activated {
			return Ok(name + " already activated"), nil
		}
		if isFreeBSD && mounted {
			return Fail("Unable to activate mounted BE!"), nil
		}
		if _, err := run(ctx, conn, "beadm activate "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " activated"), nil

	case "mounted":
		if mounted {
			return Ok(name + " already mounted"), nil
		}
		cmd := "beadm mount " + shellQuote(name)
		if mountpoint != "" {
			cmd += " " + shellQuote(mountpoint)
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(name + " mounted"), nil

	case "unmounted":
		if !mounted {
			return Ok(name + " already unmounted"), nil
		}
		cmd := "beadm unmount"
		if force {
			cmd += " -f"
		}
		cmd += " " + shellQuote(name)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(name + " unmounted"), nil

	default:
		return Result{}, errArg("beadm: state must be absent, activated, mounted, present, or unmounted, got %q", state)
	}
}

func beadmIsFreeBSD(ctx context.Context, conn remoteexec.Connection) (bool, error) {
	out, err := run(ctx, conn, "uname -s")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "FreeBSD", nil
}

func beadmFindByName(listOut, name string, isFreeBSD bool) (fields []string, found bool) {
	hasAt := strings.Contains(name, "@")
	for _, line := range splitLines(listOut) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var f []string
		if isFreeBSD {
			f = strings.Fields(line)
		} else {
			f = strings.Split(line, ";")
		}
		if len(f) == 0 {
			continue
		}
		key := f[0]
		if isFreeBSD && hasAt {
			parts := strings.Split(f[0], "/")
			key = parts[len(parts)-1]
		}
		if key == name {
			return f, true
		}
	}
	return nil, false
}

func beadmIsActivated(fields []string, isFreeBSD bool) bool {
	idx := 2
	if isFreeBSD {
		idx = 1
	}
	if idx >= len(fields) {
		return false
	}
	return strings.Contains(fields[idx], "R")
}

func beadmIsMounted(fields []string, isFreeBSD bool) bool {
	if isFreeBSD {
		if len(fields) < 3 {
			return false
		}
		return fields[2] != "-" && fields[2] != "/"
	}
	if len(fields) < 4 {
		return false
	}
	return fields[3] != ""
}
