package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// lxdProfile is the subset of `lxc profile show <name> --format json`
// (actually `lxc query GET /1.0/profiles/<name>`, see moduleLxdProfile's
// own doc comment) this port reads.
type lxdProfile struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Config      map[string]string            `json:"config"`
	Devices     map[string]map[string]string `json:"devices"`
}

// lxdGetProfile looks up name via `lxc query GET
// /1.0/profiles/<name>?project=<p>` — the raw LXD API passthrough
// `lxc query` command exposes, chosen over `lxc profile show` (which
// only emits YAML, not JSON) so this port can decode a profile exactly
// as pylxd's own REST client would see it. Returns (nil, nil) if the
// profile does not exist (a 404, surfaced by `lxc query` as a non-zero
// exit and an error message on stdout, not stderr).
func lxdGetProfile(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string) (*lxdProfile, error) {
	path := "/1.0/profiles/" + name
	if p := argString(args, "project", ""); p != "" {
		path += "?project=" + p
	}
	argv := []string{lxdBin, "query", "GET", path}
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return nil, fmt.Errorf("lxd_profile: running lxc query: %w", err)
	}
	if res.RC != 0 {
		return nil, nil
	}
	var p lxdProfile
	if err := json.Unmarshal([]byte(res.Stdout), &p); err != nil {
		return nil, fmt.Errorf("lxd_profile: parsing lxc query output: %w", err)
	}
	return &p, nil
}

// moduleLxdProfile implements Ansible's `lxd_profile`
// (community.general) module: manages an LXD profile (a reusable
// config/devices template instances can be attached to) via the `lxc`
// CLI — see lxdBin's own doc comment for why this port substitutes the
// CLI for real lxd_profile's pylxd REST client, and lxdGetProfile's own
// doc comment for why reads go through `lxc query` (the raw API
// passthrough) rather than `lxc profile show`'s YAML-only output.
//
// Args: name (required); state (present|absent, default "present");
// description; config (map[string]string); devices (map of
// device-name to a dict with a required "type" key plus that device's
// own config, matching real lxd_profile's own shape, applied only at
// creation — see lxdReconcileConfig's own doc comment for why an
// existing profile's `devices` are not reconciled by this port
// either); merge_profile (bool, default false) — real lxd_profile
// documents this as choosing between replacing the whole profile
// config and merging into it; this port always reconciles `config`
// key-by-key via `lxc profile set` regardless of merge_profile (it has
// no bulk "replace" primitive to fall back to without a hand-rolled
// YAML document for `lxc profile edit`), so merge_profile is accepted
// for compatibility but only changes WHICH keys are compared: false
// (the default) requires every key in the desired `config` and the
// existing profile's config to match as a whole set before being
// considered unchanged, while true only checks the desired keys
// individually — neither path removes a key present on the existing
// profile but absent from the desired `config`, which is the one
// concrete behavioral gap from real lxd_profile's own "replace"
// semantics; new_name — if set and a profile named `name` exists,
// `lxc profile rename` runs first, and the rest of this module's own
// reconciliation then targets new_name; project — see
// lxdProjectArgs.
//
// Idempotency: a profile that already exists and whose
// description/config already match the desired values is reported
// unchanged; otherwise state=present reconciles as described above.
// state=absent deletes an existing profile via `lxc profile delete`,
// or reports unchanged if it doesn't exist — matching real
// lxd_profile's own documented "a name that already existed... simply
// returns as unchanged" note (for creation) mirrored here for deletion
// of a name that never existed.
func moduleLxdProfile(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("lxd_profile: state must be present or absent, got %q", state)
	}

	renamed := false
	if newName := argString(args, "new_name", ""); newName != "" && state == "present" {
		existing, err := lxdGetProfile(ctx, conn, args, name)
		if err != nil {
			return Result{}, err
		}
		if existing != nil {
			argv := append([]string{lxdBin, "profile", "rename", name, newName}, lxdProjectArgs(args)...)
			if res, err := lxdRun(ctx, conn, argv); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("lxd_profile: renaming " + name + " to " + newName + ": " + strings.TrimSpace(res.Stderr)), nil
			}
			name = newName
			renamed = true
		}
	}

	existing, err := lxdGetProfile(ctx, conn, args, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if existing == nil {
			return Ok(name + " already absent"), nil
		}
		argv := append([]string{lxdBin, "profile", "delete", name}, lxdProjectArgs(args)...)
		if res, err := lxdRun(ctx, conn, argv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxd_profile: deleting " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(name + " deleted"), nil
	}

	description := argString(args, "description", "")
	desiredConfig := argMapString(args, "config")
	desiredDevices := argMapStringMap(args, "devices")

	if existing == nil {
		argv := []string{lxdBin, "profile", "create", name}
		argv = append(argv, lxdProjectArgs(args)...)
		if res, err := lxdRun(ctx, conn, argv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxd_profile: creating " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		if description != "" {
			if err := lxdProfileSetDescription(ctx, conn, args, name, description); err != nil {
				return Result{}, err
			}
		}
		keys := make([]string, 0, len(desiredConfig))
		for k := range desiredConfig {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := lxdProfileSetConfig(ctx, conn, args, name, k, desiredConfig[k]); err != nil {
				return Result{}, err
			}
		}
		devNames := make([]string, 0, len(desiredDevices))
		for dn := range desiredDevices {
			devNames = append(devNames, dn)
		}
		sort.Strings(devNames)
		for _, dn := range devNames {
			if err := lxdAddDevice(ctx, conn, args, name, dn, desiredDevices[dn]); err != nil {
				return Result{}, err
			}
		}
		return Changed(name + " created"), nil
	}

	changed := renamed
	if description != "" && description != existing.Description {
		if err := lxdProfileSetDescription(ctx, conn, args, name, description); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if argBool(args, "merge_profile", false) {
		keys := make([]string, 0, len(desiredConfig))
		for k := range desiredConfig {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if existing.Config[k] == desiredConfig[k] {
				continue
			}
			if err := lxdProfileSetConfig(ctx, conn, args, name, k, desiredConfig[k]); err != nil {
				return Result{}, err
			}
			changed = true
		}
	} else if len(desiredConfig) > 0 && !stringMapsEqual(existing.Config, desiredConfig) {
		keys := make([]string, 0, len(desiredConfig))
		for k := range desiredConfig {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := lxdProfileSetConfig(ctx, conn, args, name, k, desiredConfig[k]); err != nil {
				return Result{}, err
			}
		}
		changed = true
	}

	return Result{Changed: changed, Msg: name}, nil
}

func lxdProfileSetDescription(ctx context.Context, conn remoteexec.Connection, args map[string]any, name, description string) error {
	argv := append([]string{lxdBin, "profile", "set", name, "description", description}, lxdProjectArgs(args)...)
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("lxd_profile: setting description on %s: %s", name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func lxdProfileSetConfig(ctx context.Context, conn remoteexec.Connection, args map[string]any, name, key, value string) error {
	argv := append([]string{lxdBin, "profile", "set", name, key, value}, lxdProjectArgs(args)...)
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("lxd_profile: setting %s=%s on %s: %s", key, value, name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
