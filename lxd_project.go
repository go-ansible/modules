package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// lxdProject is the subset of `lxc query GET /1.0/projects/<name>`
// this port reads — see lxdGetProfile's own doc comment (lxd_profile.go)
// for why this family of modules reads through `lxc query` rather than
// a subcommand's own human-oriented output.
type lxdProject struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Config      map[string]string `json:"config"`
}

func lxdGetProject(ctx context.Context, conn remoteexec.Connection, name string) (*lxdProject, error) {
	argv := []string{lxdBin, "query", "GET", "/1.0/projects/" + name}
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return nil, fmt.Errorf("lxd_project: running lxc query: %w", err)
	}
	if res.RC != 0 {
		return nil, nil
	}
	var p lxdProject
	if err := json.Unmarshal([]byte(res.Stdout), &p); err != nil {
		return nil, fmt.Errorf("lxd_project: parsing lxc query output: %w", err)
	}
	return &p, nil
}

// moduleLxdProject implements Ansible's `lxd_project`
// (community.general) module: manages an LXD project (a namespace that
// isolates its own instances/profiles/networks/storage volumes from
// every other project on the same server) via the `lxc` CLI — see
// lxdBin's own doc comment for why this port substitutes the CLI for
// real lxd_project's pylxd REST client.
//
// Args: name (required); state (present|absent, default "present");
// description; config (map[string]string, e.g.
// `{"features.profiles": "true"}`) — reconciled key-by-key via `lxc
// project set`, the same narrower merge lxd_profile.go's own
// moduleLxdProfile documents (real lxd_project's own "if configuration
// is the same after merged, no change is made" wording for
// merge_project applies identically here); merge_project (bool,
// default false) — see moduleLxdProfile's own doc comment on
// merge_profile for the identical behavioral gap in this port (no
// bulk-replace primitive, so this only changes which keys are compared
// for the unchanged check, never removes a key absent from the
// desired `config`); new_name — if set and a project named `name`
// exists, `lxc project rename` runs first.
//
// Idempotency: matches lxd_profile.go's own moduleLxdProfile exactly
// (project is structurally identical to profile here — name,
// description, a flat string config, no devices).
//
// Note carried over from real lxd_project's own documented behavior:
// deleting the built-in "default" project, or a project that still has
// instances/images/profiles in it, fails at the `lxc` CLI level; this
// port surfaces that failure via Result{Failed:true} with `lxc project
// delete`'s own stderr rather than pre-checking it itself.
func moduleLxdProject(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("lxd_project: state must be present or absent, got %q", state)
	}

	renamed := false
	if newName := argString(args, "new_name", ""); newName != "" && state == "present" {
		existing, err := lxdGetProject(ctx, conn, name)
		if err != nil {
			return Result{}, err
		}
		if existing != nil {
			argv := []string{lxdBin, "project", "rename", name, newName}
			if res, err := lxdRun(ctx, conn, argv); err != nil {
				return Result{}, err
			} else if res.RC != 0 {
				return Fail("lxd_project: renaming " + name + " to " + newName + ": " + strings.TrimSpace(res.Stderr)), nil
			}
			name = newName
			renamed = true
		}
	}

	existing, err := lxdGetProject(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if existing == nil {
			return Ok(name + " already absent"), nil
		}
		argv := []string{lxdBin, "project", "delete", name}
		if res, err := lxdRun(ctx, conn, argv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxd_project: deleting " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(name + " deleted"), nil
	}

	description := argString(args, "description", "")
	desiredConfig := argMapString(args, "config")

	if existing == nil {
		argv := []string{lxdBin, "project", "create", name}
		if res, err := lxdRun(ctx, conn, argv); err != nil {
			return Result{}, err
		} else if res.RC != 0 {
			return Fail("lxd_project: creating " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		if description != "" {
			if err := lxdProjectSetDescription(ctx, conn, name, description); err != nil {
				return Result{}, err
			}
		}
		keys := make([]string, 0, len(desiredConfig))
		for k := range desiredConfig {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := lxdProjectSetConfig(ctx, conn, name, k, desiredConfig[k]); err != nil {
				return Result{}, err
			}
		}
		return Changed(name + " created"), nil
	}

	changed := renamed
	if description != "" && description != existing.Description {
		if err := lxdProjectSetDescription(ctx, conn, name, description); err != nil {
			return Result{}, err
		}
		changed = true
	}

	if argBool(args, "merge_project", false) {
		keys := make([]string, 0, len(desiredConfig))
		for k := range desiredConfig {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if existing.Config[k] == desiredConfig[k] {
				continue
			}
			if err := lxdProjectSetConfig(ctx, conn, name, k, desiredConfig[k]); err != nil {
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
			if err := lxdProjectSetConfig(ctx, conn, name, k, desiredConfig[k]); err != nil {
				return Result{}, err
			}
		}
		changed = true
	}

	return Result{Changed: changed, Msg: name}, nil
}

func lxdProjectSetDescription(ctx context.Context, conn remoteexec.Connection, name, description string) error {
	argv := []string{lxdBin, "project", "set", name, "description", description}
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("lxd_project: setting description on %s: %s", name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func lxdProjectSetConfig(ctx context.Context, conn remoteexec.Connection, name, key, value string) error {
	argv := []string{lxdBin, "project", "set", name, key, value}
	res, err := lxdRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("lxd_project: setting %s=%s on %s: %s", key, value, name, strings.TrimSpace(res.Stderr))
	}
	return nil
}
