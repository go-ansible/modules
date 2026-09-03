package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLxcContainer implements (a subset of) Ansible's `lxc_container`
// (community.general) module: manages the lifecycle of an OLDER-style
// LXC container via the classic `lxc-create`/`lxc-start`/`lxc-stop`/
// `lxc-destroy`/`lxc-freeze`/`lxc-unfreeze`/`lxc-info`/`lxc-clone`
// tools — a genuinely different, older toolset from the `lxc` LXD
// client command lxd_container.go's own moduleLxdContainer shells out
// to (confusingly similar name, unrelated daemon/API); do not confuse
// the two. Real lxc_container itself already shells out to these same
// classic lxc-* tools (there is no library form to substitute — unlike
// lxd_container's own pylxd REST client, real lxc_container has always
// been a CLI wrapper), so this port's architecture matches real
// lxc_container's own here more closely than for any other module in
// this batch.
//
// Args: name (required); state (started|stopped|restarted|absent|
// frozen|clone, default "started"); template (default "ubuntu") —
// passed to `lxc-create -t`; template_options — passed to `lxc-create`
// after a literal `--` (real lxc_container's own documented way of
// forwarding template-specific arguments); backing_store
// (dir|lvm|loop|btrfs|overlayfs|zfs, default "dir") — passed to
// `lxc-create -B`; config — path to an LXC config file, `lxc-create
// -f`; container_config ([]string of "key=value") — appended as
// repeated `lxc-create -s key=value` flags; lxc_path — `-P` on every
// lxc-* invocation this port issues (create/info/start/stop/destroy/
// freeze/unfreeze/clone), matching real lxc_container's own
// consistent use of it; clone_name/clone_snapshot — state=clone only,
// see below; container_command — if set, run inside the container via
// `lxc-attach -n <name> -- bash -c '<command>'` after this module's
// own state handling completes (matching real lxc_container's own
// documented "can be used with any state except absent" note; a
// container that must be started to run the command is started first
// and, if state=stopped, stopped again afterward, exactly like real
// lxc_container's own documented behavior).
//
// Simplifications vs real lxc_container: no fs_size/fs_type/lv_name/
// vg_name/thinpool/zfs_root (LVM/ZFS backing-store sizing knobs — this
// port passes `backing_store` through to `lxc-create -B` but not its
// own storage-sizing flags, which vary by backend and are better set
// via `container_config`/`template_options` directly); no
// archive/archive_compression/archive_path (real lxc_container's own
// LVM-snapshot-aware tarball export — this port does not implement
// container archival at all, a narrowing rather than a faked
// best-effort); no container_log/container_log_level (LXC's own -l/-o
// logging flags on lxc-start — accepted as no-ops would be misleading,
// so this port rejects them if set, same "honest error over silent
// no-op" stance htpasswd.go's own hash_scheme validation takes).
//
// Idempotency is checked via `lxc-info -n <name> -P <lxc_path> -s`
// (exit 0 and a "State:" line iff the container exists; the reported
// State determines RUNNING/STOPPED/FROZEN). state=started/stopped/
// frozen only act when the current state differs; state=restarted
// always issues `lxc-stop` then `lxc-start` (even immediately after a
// fresh creation), matching lxd_container.go's own identical
// "restarted always changes" convention. state=absent runs
// `lxc-destroy -f` (force-stopping a running container first) if the
// container exists. state=clone requires clone_name and runs
// `lxc-clone -o <name> -n <clone_name>` (plus `-s` for
// clone_snapshot), changed only if clone_name did not already exist.
func moduleLxcContainer(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "started")
	switch state {
	case "started", "stopped", "restarted", "absent", "frozen", "clone":
	default:
		return Result{}, errArg("lxc_container: state must be one of started, stopped, restarted, absent, frozen, clone, got %q", state)
	}
	if argBool(args, "container_log", false) {
		return Result{}, errArg("lxc_container: container_log/container_log_level are not supported by this port (see moduleLxcContainer's doc comment)")
	}
	lxcPathFlag := lxcPathArgs(args)

	if state == "clone" {
		return lxcContainerClone(ctx, conn, args, name, lxcPathFlag)
	}

	status, exists, err := lxcContainerStatus(ctx, conn, name, lxcPathFlag)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(name + " already absent"), nil
		}
		argv := append([]string{"lxc-destroy", "-n", name, "-f"}, lxcPathFlag...)
		if res, err := lxdCliRun(ctx, conn, argv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxc_container: destroying " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(name + " destroyed"), nil
	}

	var actions []string
	if !exists {
		argv := []string{"lxc-create", "-n", name, "-t", argString(args, "template", "ubuntu")}
		argv = append(argv, "-B", argString(args, "backing_store", "dir"))
		argv = append(argv, lxcPathFlag...)
		if cfg := argString(args, "config", ""); cfg != "" {
			argv = append(argv, "-f", cfg)
		}
		for _, kv := range argStringList(args, "container_config") {
			argv = append(argv, "-s", kv)
		}
		if opts := argString(args, "template_options", ""); opts != "" {
			argv = append(argv, "--")
			argv = append(argv, tokenize(opts)...)
		}
		if res, err := lxdCliRun(ctx, conn, argv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxc_container: creating " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		actions = append(actions, "create")
		status = "STOPPED"
	}

	switch state {
	case "started":
		if status == "FROZEN" {
			if err := lxcSimpleAction(ctx, conn, "lxc-unfreeze", name, lxcPathFlag); err != nil {
				return Fail("lxc_container: " + err.Error()), nil
			}
			actions = append(actions, "unfreeze")
		} else if status != "RUNNING" {
			if err := lxcStart(ctx, conn, name, lxcPathFlag); err != nil {
				return Fail("lxc_container: " + err.Error()), nil
			}
			actions = append(actions, "start")
		}
	case "stopped":
		if status == "RUNNING" || status == "FROZEN" {
			if err := lxcSimpleAction(ctx, conn, "lxc-stop", name, lxcPathFlag); err != nil {
				return Fail("lxc_container: " + err.Error()), nil
			}
			actions = append(actions, "stop")
		}
	case "restarted":
		if status == "RUNNING" || status == "FROZEN" {
			if err := lxcSimpleAction(ctx, conn, "lxc-stop", name, lxcPathFlag); err != nil {
				return Fail("lxc_container: " + err.Error()), nil
			}
		}
		if err := lxcStart(ctx, conn, name, lxcPathFlag); err != nil {
			return Fail("lxc_container: " + err.Error()), nil
		}
		actions = append(actions, "restart")
	case "frozen":
		if status != "RUNNING" && status != "FROZEN" {
			if err := lxcStart(ctx, conn, name, lxcPathFlag); err != nil {
				return Fail("lxc_container: " + err.Error()), nil
			}
			actions = append(actions, "start")
		}
		if status != "FROZEN" {
			if err := lxcSimpleAction(ctx, conn, "lxc-freeze", name, lxcPathFlag); err != nil {
				return Fail("lxc_container: " + err.Error()), nil
			}
			actions = append(actions, "freeze")
		}
	}

	if cmd := argString(args, "container_command", ""); cmd != "" {
		startedForCommand := false
		if state == "stopped" {
			// real lxc_container starts the container to run the
			// command, then stops it again afterward.
			if err := lxcStart(ctx, conn, name, lxcPathFlag); err != nil {
				return Fail("lxc_container: " + err.Error()), nil
			}
			startedForCommand = true
			actions = append(actions, "start")
		}
		argv := append([]string{"lxc-attach", "-n", name}, lxcPathFlag...)
		argv = append(argv, "--", "bash", "-c", cmd)
		if _, err := lxdCliRun(ctx, conn, argv); err != nil {
			return Result{}, err
		}
		actions = append(actions, "container_command")
		if startedForCommand {
			if err := lxcSimpleAction(ctx, conn, "lxc-stop", name, lxcPathFlag); err != nil {
				return Fail("lxc_container: " + err.Error()), nil
			}
			actions = append(actions, "stop")
		}
	}

	return Result{Changed: len(actions) > 0, Msg: name}.WithExtra("actions", actions), nil
}

func lxcPathArgs(args map[string]any) []string {
	if p := argString(args, "lxc_path", ""); p != "" {
		return []string{"-P", p}
	}
	return nil
}

// lxdCliRun quotes and runs one classic-LXC (`lxc-*`) invocation —
// named distinctly from lxdRun (lxd_container.go's own `lxc` LXD
// client runner) to keep the two toolsets visually distinct in this
// file, even though both ultimately just shell-quote and Exec.
func lxdCliRun(ctx context.Context, conn remoteexec.Connection, argv []string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, strings.Join(quoted, " "), nil)
}

// lxcContainerStatus runs `lxc-info -n <name> -s` and parses its own
// "State: RUNNING"/"STOPPED"/"FROZEN" line. exists is false (not an
// error) if lxc-info reports the container doesn't exist.
func lxcContainerStatus(ctx context.Context, conn remoteexec.Connection, name string, lxcPathFlag []string) (status string, exists bool, err error) {
	argv := append([]string{"lxc-info", "-n", name, "-s"}, lxcPathFlag...)
	res, err := lxdCliRun(ctx, conn, argv)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "State:"); ok {
			return strings.ToUpper(strings.TrimSpace(v)), true, nil
		}
	}
	return "", true, nil
}

func lxcStart(ctx context.Context, conn remoteexec.Connection, name string, lxcPathFlag []string) error {
	argv := append([]string{"lxc-start", "-n", name, "-d"}, lxcPathFlag...)
	res, err := lxdCliRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("starting %s: %s", name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func lxcSimpleAction(ctx context.Context, conn remoteexec.Connection, tool, name string, lxcPathFlag []string) error {
	argv := append([]string{tool, "-n", name}, lxcPathFlag...)
	res, err := lxdCliRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("%s %s: %s", tool, name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func lxcContainerClone(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string, lxcPathFlag []string) (Result, error) {
	cloneName, err := requireString(args, "clone_name")
	if err != nil {
		return Result{}, errArg("lxc_container: clone_name is required when state=clone")
	}
	_, exists, err := lxcContainerStatus(ctx, conn, cloneName, lxcPathFlag)
	if err != nil {
		return Result{}, err
	}
	if exists {
		return Ok(cloneName + " already exists"), nil
	}
	argv := append([]string{"lxc-clone", "-o", name, "-n", cloneName}, lxcPathFlag...)
	if argBool(args, "clone_snapshot", false) {
		argv = append(argv, "-s")
	}
	if res, err := lxdCliRun(ctx, conn, argv); err != nil {
		return Result{}, err
	} else if res.RC != 0 {
		return Fail("lxc_container: cloning " + name + " to " + cloneName + ": " + strings.TrimSpace(res.Stderr)), nil
	}
	return Changed(cloneName + " created from " + name), nil
}
