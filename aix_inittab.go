package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAixInittab implements Ansible's `aix_inittab` module
// (community.general, deprecated upstream in favor of
// `ibm.power_aix.inittab`): adds, changes, or removes an entry in
// AIX's `/etc/inittab` via `lsitab`/`mkitab`/`chitab`/`rmitab`.
//
// Args: name (string, required, alias service); runlevel (string,
// required); command (string, required); action (string, optional —
// one of boot|bootwait|hold|initdefault|off|once|ondemand|powerfail|
// powerwait|respawn|sysinit|wait); insertafter (string, optional) —
// `mkitab -i <insertafter>`, only used when creating a NEW entry;
// state (present|absent, default "present").
//
// An existing entry is read via `lsitab <name>` (rc==0 means present;
// its colon-separated "name:runlevel:action:command" output is parsed
// into fields, missing trailing fields treated as ""). state=present
// compares runlevel/action/command against the current entry: if all
// three already match, this is a no-op; if the entry exists but
// differs, it is updated via `chitab "name:runlevel:action:command"`;
// if it doesn't exist, it is added via `mkitab` (with `-i insertafter`
// when given).
//
// Deviation from real aix_inittab.py: `action` is not marked required
// in real aix_inittab's own argspec, yet its `new_entry =
// f"{name}:{runlevel}:{action}:{command}"` would insert the literal
// text "None" into `/etc/inittab` if action were omitted (Python's
// str.format of None); this port instead treats a missing action as
// an empty field ("name:runlevel::command"), avoiding writing the
// word "None" into a live system file — a deliberate improvement over
// what real aix_inittab.py's own f-string would literally produce.
func moduleAixInittab(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", argString(args, "service", ""))
	if name == "" {
		return Result{}, errArg("aix_inittab: missing required argument: name (or its alias service)")
	}
	runlevel, err := requireString(args, "runlevel")
	if err != nil {
		return Result{}, err
	}
	command, err := requireString(args, "command")
	if err != nil {
		return Result{}, err
	}
	action := argString(args, "action", "")
	insertafter := argString(args, "insertafter", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("aix_inittab: state must be present or absent, got %q", state)
	}

	cur, exists, err := aixInittabCurrent(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	if state == "present" {
		newEntry := name + ":" + runlevel + ":" + action + ":" + command
		if exists && cur.runlevel == runlevel && cur.action == action && cur.command == command {
			return Ok(name).WithExtra("name", name), nil
		}
		if exists {
			if _, err := run(ctx, conn, "chitab "+shellQuote(newEntry)); err != nil {
				return Result{}, err
			}
			return Changed("changed inittab entry "+name).WithExtra("name", name), nil
		}
		cmd := "mkitab"
		if insertafter != "" {
			cmd += " -i " + shellQuote(insertafter)
		}
		cmd += " " + shellQuote(newEntry)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("add inittab entry "+name).WithExtra("name", name), nil
	}

	if !exists {
		return Ok(name+" already absent").WithExtra("name", name), nil
	}
	if _, err := run(ctx, conn, "rmitab "+shellQuote(name)); err != nil {
		return Result{}, err
	}
	return Changed("removed inittab entry "+name).WithExtra("name", name), nil
}

type aixInittabEntry struct {
	runlevel, action, command string
}

func aixInittabCurrent(ctx context.Context, conn remoteexec.Connection, name string) (aixInittabEntry, bool, error) {
	res, err := runStatus(ctx, conn, "lsitab "+shellQuote(name))
	if err != nil {
		return aixInittabEntry{}, false, err
	}
	if res.RC != 0 {
		return aixInittabEntry{}, false, nil
	}
	parts := strings.SplitN(strings.TrimRight(res.Stdout, "\n"), ":", 4)
	for len(parts) < 4 {
		parts = append(parts, "")
	}
	return aixInittabEntry{
		runlevel: strings.TrimSpace(parts[1]),
		action:   strings.TrimSpace(parts[2]),
		command:  strings.TrimSpace(parts[3]),
	}, true, nil
}
