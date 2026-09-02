package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUser implements (a subset of) Ansible's `user` module.
//
// Args: name (string, required); state (present|absent, default
// "present"); shell; home; groups ([]string, supplementary groups via
// usermod -G); system (bool) — pass -r/-system to useradd;
// create_home (bool, default true).
func moduleUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")

	exists, err := userExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(name + " already absent"), nil
		}
		if _, err := run(ctx, conn, "userdel -r "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed"), nil
	}

	shell := argString(args, "shell", "")
	home := argString(args, "home", "")
	groups := argStringList(args, "groups")
	system := argBool(args, "system", false)
	createHome := argBool(args, "create_home", true)

	if !exists {
		var b strings.Builder
		b.WriteString("useradd")
		if !createHome {
			b.WriteString(" -M")
		} else {
			b.WriteString(" -m")
		}
		if system {
			b.WriteString(" -r")
		}
		if shell != "" {
			b.WriteString(" -s " + shellQuote(shell))
		}
		if home != "" {
			b.WriteString(" -d " + shellQuote(home))
		}
		if len(groups) > 0 {
			b.WriteString(" -G " + shellQuote(strings.Join(groups, ",")))
		}
		b.WriteString(" " + shellQuote(name))
		if _, err := run(ctx, conn, b.String()); err != nil {
			return Result{}, err
		}
		return Changed(name + " created"), nil
	}

	// Already exists: apply any requested changes via usermod. We don't
	// probe current shell/home/groups (no fully portable single-command
	// read across distros without parsing /etc/passwd, which getent
	// does but still requires parsing) — a usermod call is a no-op on
	// the OS side when the value is unchanged, but this always issues
	// it when any of these args is set, so it is reported changed
	// whenever they are, matching the same honest limitation as `file`'s
	// owner/group handling.
	var mods []string
	if shell != "" {
		mods = append(mods, "-s "+shellQuote(shell))
	}
	if home != "" {
		mods = append(mods, "-d "+shellQuote(home))
	}
	if len(groups) > 0 {
		mods = append(mods, "-G "+shellQuote(strings.Join(groups, ",")))
	}
	if len(mods) == 0 {
		return Ok(name + " unchanged"), nil
	}
	cmd := "usermod " + strings.Join(mods, " ") + " " + shellQuote(name)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed(name), nil
}

func userExists(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := conn.Exec(ctx, "getent passwd "+shellQuote(name), nil)
	if err != nil {
		return false, fmt.Errorf("checking user %s: %w", name, err)
	}
	return res.RC == 0, nil
}
