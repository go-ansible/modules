package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// lxdBin is the command-line client this port shells out to for every
// lxd_*.go module in this file's family. Real community.general's LXD
// modules (lxd_container, lxd_profile, lxd_project,
// lxd_storage_pool_info, lxd_storage_volume_info) are all implemented
// against pylxd's own REST client, speaking the LXD HTTP API directly
// (over a Unix socket or HTTPS, selected by the `url` argument) — they
// never shell out to any binary at all. This port has no Go LXD client
// wired into remoteexec.Connection, so it substitutes the `lxc`
// command-line client instead: the same tool a human operator normally
// has on a box already talking to a running LXD (or Incus, which ships
// a compatible `lxc` alias/symlink for its own daemon) — matching the
// substitution this project already makes for consul_kv.go (consul
// CLI) and terraform.go (terraform CLI itself). See each module's own
// doc comment for the specific fidelity gaps this implies.
const lxdBin = "lxc"

// lxdInstance is the subset of `lxc list <name> --format json`'s own
// per-instance object this port reads — a shape that lines up with the
// LXD REST API's own instance representation (GET /1.0/instances/<name>
// with recursion), which is the closest a CLI substitution gets to
// pylxd's own native shape.
type lxdInstance struct {
	Name     string                       `json:"name"`
	Status   string                       `json:"status"`
	Type     string                       `json:"type"`
	Profiles []string                     `json:"profiles"`
	Config   map[string]string            `json:"config"`
	Devices  map[string]map[string]string `json:"devices"`
	State    *lxdInstanceState            `json:"state"`
}

type lxdInstanceState struct {
	Network map[string]struct {
		Addresses []struct {
			Family  string `json:"family"`
			Address string `json:"address"`
		} `json:"addresses"`
	} `json:"network"`
}

// lxdRun quotes and runs one `lxc` invocation.
func lxdRun(ctx context.Context, conn remoteexec.Connection, argv []string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, strings.Join(quoted, " "), nil)
}

// lxdProjectArgs returns `--project <p>` when the module's `project`
// argument is set, appended to nearly every `lxc` invocation in this
// file's family — matching real lxd_*'s own `project` option.
//
// Deviation shared by every module in this family: real lxd_container/
// lxd_profile/lxd_project/lxd_storage_*_info also accept url/
// trust_password/client_cert/client_key/snap_url to select and
// authenticate against a specific LXD server over pylxd's own REST
// client. The `lxc` CLI has no per-invocation equivalent — it always
// talks to whichever server its own `lxc remote` configuration already
// points at (locally, normally the local LXD/Incus socket). This port
// accepts those arguments (so existing playbooks don't fail argument
// validation) but ignores them entirely; connecting to a specific
// remote LXD server means configuring `lxc remote add`/`lxc remote
// switch` on the target out of band, not through this module.
func lxdProjectArgs(args map[string]any) []string {
	if p := argString(args, "project", ""); p != "" {
		return []string{"--project", p}
	}
	return nil
}

// lxdGetInstance looks up name via `lxc list <name> --format json`,
// returning (nil, nil) if it does not exist.
func lxdGetInstance(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string) (*lxdInstance, error) {
	argv := append([]string{lxdBin, "list", name, "--format", "json"}, lxdProjectArgs(args)...)
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return nil, fmt.Errorf("lxd: running lxc list: %w", err)
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("lxd: lxc list %s failed: %s", name, strings.TrimSpace(res.Stderr))
	}
	var list []lxdInstance
	if err := json.Unmarshal([]byte(res.Stdout), &list); err != nil {
		return nil, fmt.Errorf("lxd: parsing lxc list output: %w", err)
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i], nil
		}
	}
	return nil, nil
}

// lxdWaitForIPv4 polls lxdGetInstance once a second until every network
// device on the instance has at least one "inet" address, or timeoutSec
// elapses — matching real lxd_container's own wait_for_ipv4_addresses,
// except this port cannot distinguish "still booting" from "will never
// get an address" any better than real lxd_container's own identical
// polling loop does; on timeout this returns whatever (possibly empty)
// addresses were last observed rather than failing the task, matching
// real lxd_container's own tolerant behavior there too.
func lxdWaitForIPv4(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string, timeoutSec int) (map[string][]string, error) {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for {
		inst, err := lxdGetInstance(ctx, conn, args, name)
		if err != nil {
			return nil, err
		}
		addrs := map[string][]string{}
		if inst != nil && inst.State != nil {
			for dev, net := range inst.State.Network {
				for _, a := range net.Addresses {
					if a.Family == "inet" {
						addrs[dev] = append(addrs[dev], a.Address)
					}
				}
			}
		}
		if len(addrs) > 0 || time.Now().After(deadline) {
			return addrs, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// argMapStringMap reads a module argument as a nested
// map[string]map[string]string — used for lxd_container/lxd_profile's
// own `devices` (each device's own key/value config, plus its "type").
func argMapStringMap(args map[string]any, key string) map[string]map[string]string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]map[string]string, len(m))
	for name, val := range m {
		inner, ok := val.(map[string]any)
		if !ok {
			continue
		}
		out[name] = make(map[string]string, len(inner))
		for k, v := range inner {
			out[name][k] = fmt.Sprint(v)
		}
	}
	return out
}

// lxdAddDevice runs `lxc config device add <name> <devName> <type>
// [key=value...]`, matching real lxd_container/lxd_profile's own
// `devices` dict shape (a "type" key plus arbitrary other config keys,
// mirroring the LXD API's own device object).
func lxdAddDevice(ctx context.Context, conn remoteexec.Connection, args map[string]any, target, devName string, dev map[string]string) error {
	devType := dev["type"]
	if devType == "" {
		return errArg("lxd: device %q is missing a required \"type\" field", devName)
	}
	argv := []string{lxdBin, "config", "device", "add", target, devName, devType}
	keys := make([]string, 0, len(dev))
	for k := range dev {
		if k == "type" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		argv = append(argv, k+"="+dev[k])
	}
	argv = append(argv, lxdProjectArgs(args)...)
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("lxd: adding device %s to %s: %s", devName, target, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// lxdReconcileConfig applies every key in the module's `config`
// argument that differs from inst.Config via `lxc config set`, sorted
// for deterministic command ordering.
//
// Deviation from real lxd_container/lxd_profile: real modules also
// detect and reconcile `profiles`/`devices` drift on an *already
// existing* instance/profile (and the `ignore_volatile_options`
// argument governs whether auto-generated `volatile.*` config keys
// participate in that comparison). This port only applies
// profiles/devices at creation time — reconciling them on an existing
// instance would need a full desired-vs-actual diff (including
// removals) this port does not implement; changing profiles/devices on
// an existing instance/profile needs a manual `lxc profile`/`lxc config
// device` command, or delete-and-recreate, exactly like
// htpasswd.go's own hash_scheme narrowing. Because this narrower
// reconciliation only ever walks the *desired* config map,
// `ignore_volatile_options` has no observable effect here (a
// `volatile.*` key never shows up unless the caller explicitly put it
// in `config`) — accepted as an argument for compatibility, documented
// as a no-op in this port.
func lxdReconcileConfig(ctx context.Context, conn remoteexec.Connection, args map[string]any, target string, current map[string]string) (bool, error) {
	desired := argMapString(args, "config")
	if len(desired) == 0 {
		return false, nil
	}
	keys := make([]string, 0, len(desired))
	for k := range desired {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	changed := false
	for _, k := range keys {
		v := desired[k]
		if current[k] == v {
			continue
		}
		argv := append([]string{lxdBin, "config", "set", target, k, v}, lxdProjectArgs(args)...)
		res, err := lxdRun(ctx, conn, argv)
		if err != nil {
			return changed, err
		}
		if res.RC != 0 {
			return changed, fmt.Errorf("lxd: setting config %s=%s on %s: %s", k, v, target, strings.TrimSpace(res.Stderr))
		}
		changed = true
	}
	return changed, nil
}

// moduleLxdContainer implements Ansible's `lxd_container`
// (community.general) module: manages the lifecycle of an LXD instance
// (container or virtual machine) via the `lxc` CLI — see lxdBin's own
// doc comment for why this port substitutes the CLI for real
// lxd_container's pylxd REST client.
//
// Args: name (required); state (started|stopped|restarted|absent|
// frozen, default "started"); type (container|virtual-machine, default
// "container") — passed as `--vm` to `lxc init` when
// "virtual-machine"; source (map, only used at creation) — this port
// only supports `source.type: image` via `source.alias` (the plain
// image name/alias passed straight to `lxc init <alias> <name>`);
// real lxd_container's own `source.server`/`protocol`/`mode`/
// `fingerprint` and its "copy"/"migration"/"none" source types are NOT
// translated into `lxc remote` configuration by this port — an alias
// resolves against whatever image remote is already the target's own
// `lxc` default (matching how a human running `lxc launch <alias>
// <name>` at a shell would expect it to resolve); profiles ([]string,
// creation only — see lxdReconcileConfig's own doc comment); config
// (map[string]string) — applied at creation via `-c k=v`, and
// reconciled (existing instances only, key-by-key `lxc config set`) via
// lxdReconcileConfig, whose doc comment covers this port's narrower
// profiles/devices handling and why `ignore_volatile_options` is
// accepted but a no-op here; devices (map of device-name to a dict with
// a required "type" key plus that device's own config, creation only);
// ephemeral (bool, creation only, `--ephemeral`); project — see
// lxdProjectArgs; target — `--target <t>` at creation, for cluster
// deployments; timeout (default 30) — seconds passed to `lxc
// stop`/`lxc restart --timeout`, and the wait_for_ipv4_addresses poll
// bound (see lxdWaitForIPv4); force_stop (bool) — `--force` on
// stop/restart; wait_for_container (bool) — accepted for
// compatibility but a no-op: every `lxc` subcommand this port invokes
// (init/start/stop/restart/freeze/unfreeze/delete) already blocks
// until its own operation completes, unlike pylxd's own async
// operations that real lxd_container's wait_for_container explicitly
// waits on; wait_for_ipv4_addresses (bool) — see lxdWaitForIPv4;
// architecture — NOT supported by this port (no `lxc init` flag
// carries it) and is silently ignored, a narrowing from real
// lxd_container's own API-level architecture override.
//
// State semantics: an instance that doesn't exist yet is always created
// first via `lxc init` (stopped), regardless of the target state, then
// this module's state machine (identical to an already-existing
// instance) drives it to started/stopped/frozen, or leaves it stopped
// for state=stopped. state=restarted always issues `lxc restart` (even
// immediately after a fresh creation), matching this project's broader
// "restarted always changes" convention (see service.go's own note,
// if present, or command.go's unconditional Changed=true). state=absent
// runs `lxc delete --force` (stopping a running instance first) if the
// instance exists, else reports unchanged.
//
// Extra["actions"]: an ordered []string of the high-level steps this
// port actually took (a subset of real lxd_container's own richer
// action log — e.g. "create", "update", "start", "stop", "restart",
// "freeze", "unfreeze", "delete"). Extra["old_state"]: the instance's
// status (lowercased) before this run, when it already existed.
// Extra["addresses"]: set only when wait_for_ipv4_addresses is true and
// state is started/restarted, a map from network device name to its
// IPv4 addresses.
func moduleLxdContainer(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "started")
	switch state {
	case "started", "stopped", "restarted", "absent", "frozen":
	default:
		return Result{}, errArg("lxd_container: state must be one of started, stopped, restarted, absent, frozen, got %q", state)
	}
	instType := argString(args, "type", "container")
	timeout := argInt(args, "timeout", 30)
	forceStop := argBool(args, "force_stop", false)

	inst, err := lxdGetInstance(ctx, conn, args, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if inst == nil {
			return Ok(name + " already absent"), nil
		}
		argv := append([]string{lxdBin, "delete", name, "--force"}, lxdProjectArgs(args)...)
		if res, err := lxdRun(ctx, conn, argv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxd_container: deleting " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(name+" deleted").WithExtra("actions", []string{"delete"}), nil
	}

	var actions []string
	oldState := ""
	if inst != nil {
		oldState = strings.ToLower(inst.Status)
	}

	if inst == nil {
		src := argMapAny(args, "source")
		alias, _ := src["alias"].(string)
		if alias == "" {
			return Result{}, errArg("lxd_container: source.alias is required to create instance %q (this port only supports source.type=image; see moduleLxdContainer's doc comment)", name)
		}
		createArgv := []string{lxdBin, "init", alias, name}
		if instType == "virtual-machine" {
			createArgv = append(createArgv, "--vm")
		}
		if argBool(args, "ephemeral", false) {
			createArgv = append(createArgv, "--ephemeral")
		}
		if target := argString(args, "target", ""); target != "" {
			createArgv = append(createArgv, "--target", target)
		}
		for _, p := range argStringList(args, "profiles") {
			createArgv = append(createArgv, "--profile", p)
		}
		desiredConfig := argMapString(args, "config")
		configKeys := make([]string, 0, len(desiredConfig))
		for k := range desiredConfig {
			configKeys = append(configKeys, k)
		}
		sort.Strings(configKeys)
		for _, k := range configKeys {
			createArgv = append(createArgv, "-c", k+"="+desiredConfig[k])
		}
		createArgv = append(createArgv, lxdProjectArgs(args)...)
		if res, err := lxdRun(ctx, conn, createArgv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxd_container: creating " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		actions = append(actions, "create")

		devNames := make([]string, 0)
		devices := argMapStringMap(args, "devices")
		for devName := range devices {
			devNames = append(devNames, devName)
		}
		sort.Strings(devNames)
		for _, devName := range devNames {
			if err := lxdAddDevice(ctx, conn, args, name, devName, devices[devName]); err != nil {
				return Result{}, err
			}
		}

		inst, err = lxdGetInstance(ctx, conn, args, name)
		if err != nil {
			return Result{}, err
		}
		if inst == nil {
			return Result{}, fmt.Errorf("lxd_container: %s not found immediately after creation", name)
		}
	} else {
		changed, err := lxdReconcileConfig(ctx, conn, args, name, inst.Config)
		if err != nil {
			return Result{}, err
		}
		if changed {
			actions = append(actions, "update")
		}
	}

	switch state {
	case "started":
		if strings.EqualFold(inst.Status, "frozen") {
			if err := lxdSimpleAction(ctx, conn, args, "unfreeze", name); err != nil {
				return Result{}, err
			}
			actions = append(actions, "unfreeze")
		} else if !strings.EqualFold(inst.Status, "running") {
			if err := lxdSimpleAction(ctx, conn, args, "start", name); err != nil {
				return Result{}, err
			}
			actions = append(actions, "start")
		}
	case "stopped":
		if strings.EqualFold(inst.Status, "running") || strings.EqualFold(inst.Status, "frozen") {
			argv := append([]string{lxdBin, "stop", name, "--timeout", strconv.Itoa(timeout)}, lxdProjectArgs(args)...)
			if forceStop {
				argv = append(argv, "--force")
			}
			if res, err := lxdRun(ctx, conn, argv); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("lxd_container: stopping " + name + ": " + strings.TrimSpace(res.Stderr)), nil
			}
			actions = append(actions, "stop")
		}
	case "restarted":
		if strings.EqualFold(inst.Status, "frozen") {
			if err := lxdSimpleAction(ctx, conn, args, "unfreeze", name); err != nil {
				return Result{}, err
			}
			actions = append(actions, "unfreeze")
		}
		argv := append([]string{lxdBin, "restart", name, "--timeout", strconv.Itoa(timeout)}, lxdProjectArgs(args)...)
		if forceStop {
			argv = append(argv, "--force")
		}
		if res, err := lxdRun(ctx, conn, argv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxd_container: restarting " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		actions = append(actions, "restart")
	case "frozen":
		if !strings.EqualFold(inst.Status, "running") {
			if err := lxdSimpleAction(ctx, conn, args, "start", name); err != nil {
				return Result{}, err
			}
			actions = append(actions, "start")
		}
		if !strings.EqualFold(inst.Status, "frozen") {
			if err := lxdSimpleAction(ctx, conn, args, "freeze", name); err != nil {
				return Result{}, err
			}
			actions = append(actions, "freeze")
		}
	}

	res := Result{Changed: len(actions) > 0}
	res = res.WithExtra("actions", actions)
	if oldState != "" {
		res = res.WithExtra("old_state", oldState)
	}
	if argBool(args, "wait_for_ipv4_addresses", false) && (state == "started" || state == "restarted") {
		addrs, err := lxdWaitForIPv4(ctx, conn, args, name, timeout)
		if err != nil {
			return Result{}, err
		}
		res = res.WithExtra("addresses", addrs)
	}
	return res, nil
}

// lxdSimpleAction runs `lxc <verb> <target>` with no extra flags.
func lxdSimpleAction(ctx context.Context, conn remoteexec.Connection, args map[string]any, verb, target string) error {
	argv := append([]string{lxdBin, verb, target}, lxdProjectArgs(args)...)
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("lxd: %s %s: %s", verb, target, strings.TrimSpace(res.Stderr))
	}
	return nil
}
