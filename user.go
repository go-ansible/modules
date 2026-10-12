package modules

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUser implements (a subset of) Ansible's `user` module.
//
// Args: name (string, required); state (present|absent, default
// "present"); uid (int) — useradd/usermod -u; non_unique (bool,
// default false) — add -o alongside -u, exactly when -u is passed;
// group (string, name or GID) — the primary group, useradd/usermod -g;
// comment (string) — the GECOS field, useradd/usermod -c; shell; home;
// groups ([]string, supplementary groups via usermod -G); system
// (bool) — pass -r/-system to useradd; create_home (bool, default
// true).
//
// group follows real Ansible's create_user_useradd: a named group that
// does not exist fails the task with real's own "Group X does not
// exist" rather than letting useradd report it. When group is NOT
// given and a group named like the user already exists, -N is passed,
// for the reason real gives — without it useradd on a USERGROUPS_ENAB
// system tries to create that group itself and fails ("group slurm
// exists - if you want to add this user to that group, use -g"). Real
// passes -n instead on RHEL 5 and older and nothing at all on SLES 11
// and older, decided by reading /etc/redhat-release and
// /etc/SuSE-release on the target; this port always passes -N, which
// is what real does on every other system. Both exceptions are long
// out of support.
//
// For an existing user, uid, group, comment, home and shell are
// compared against the user's `getent passwd` entry and only the ones
// that differ reach usermod, so the task reports changed only when one
// of them did — real's modify_user_usermod does the same comparison
// (group is compared by GID, and usermod is given the GID, as real
// does). groups is still NOT compared: real reads the user's current
// membership to decide, and this port issues -G whenever groups is set,
// so a task setting groups always reports changed. home on an existing
// user changes the passwd entry only (real's move_home is not ported).
func moduleUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")

	entry, exists, err := lookupUser(ctx, conn, name)
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

	uid, hasUID, err := argUID(args)
	if err != nil {
		return Result{}, err
	}
	nonUnique := argBool(args, "non_unique", false)
	group := argString(args, "group", "")
	// comment is compared by presence, not emptiness: real tests
	// `comment is not None`, so comment: "" clears the GECOS field.
	_, hasComment := args["comment"]
	comment := argString(args, "comment", "")
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
		if hasUID {
			b.WriteString(" -u " + strconv.Itoa(uid))
			if nonUnique {
				b.WriteString(" -o")
			}
		}
		if group != "" {
			_, found, err := lookupGroupGID(ctx, conn, group)
			if err != nil {
				return Result{}, err
			}
			if !found {
				return Fail("Group " + group + " does not exist"), nil
			}
			b.WriteString(" -g " + shellQuote(group))
		} else {
			sameName, err := groupExists(ctx, conn, name)
			if err != nil {
				return Result{}, err
			}
			if sameName {
				b.WriteString(" -N")
			}
		}
		if hasComment {
			b.WriteString(" -c " + shellQuote(comment))
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

	// Already exists: every field but groups is compared against the
	// passwd entry already read above, so it costs no extra round trip
	// (group costs one, to turn the requested name into a GID). An entry
	// that could not be parsed is an error rather than a guess, but only
	// when something actually needs comparing — a bare `user: name=x`
	// on an existing user never looks at it.
	needsEntry := hasUID || group != "" || hasComment || shell != "" || home != ""
	if needsEntry && !entry.parsed {
		return Result{}, fmt.Errorf("reading user %s: unexpected getent passwd output %q", name, entry.raw)
	}

	// Flag order follows real's modify_user_usermod: -u [-o], -g, -G,
	// -c, -d, -s.
	var mods []string
	if hasUID && entry.uid != strconv.Itoa(uid) {
		mods = append(mods, "-u "+strconv.Itoa(uid))
		if nonUnique {
			mods = append(mods, "-o")
		}
	}
	if group != "" {
		gid, found, err := lookupGroupGID(ctx, conn, group)
		if err != nil {
			return Result{}, err
		}
		if !found {
			return Fail("Group " + group + " does not exist"), nil
		}
		if entry.gid != gid {
			mods = append(mods, "-g "+shellQuote(gid))
		}
	}
	if len(groups) > 0 {
		mods = append(mods, "-G "+shellQuote(strings.Join(groups, ",")))
	}
	if hasComment && entry.comment != comment {
		mods = append(mods, "-c "+shellQuote(comment))
	}
	if home != "" && entry.home != home {
		mods = append(mods, "-d "+shellQuote(home))
	}
	if shell != "" && entry.shell != shell {
		mods = append(mods, "-s "+shellQuote(shell))
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

// passwdEntry is one `getent passwd` line, split into the fields real's
// user_info reads from pwd.getpwnam. uid and gid stay strings: they
// are compared, never computed with.
type passwdEntry struct {
	raw     string
	parsed  bool
	uid     string
	gid     string
	comment string
	home    string
	shell   string
}

// lookupUser runs `getent passwd name` once and returns both whether
// the user exists and its parsed entry. getent prints the first match
// only, and passwd(5) forbids ':' inside a field, so a plain split on
// the first line is the whole parser.
func lookupUser(ctx context.Context, conn remoteexec.Connection, name string) (passwdEntry, bool, error) {
	res, err := conn.Exec(ctx, "getent passwd "+shellQuote(name), nil)
	if err != nil {
		return passwdEntry{}, false, fmt.Errorf("checking user %s: %w", name, err)
	}
	if res.RC != 0 {
		return passwdEntry{}, false, nil
	}
	line, _, _ := strings.Cut(strings.TrimRight(res.Stdout, "\r\n"), "\n")
	e := passwdEntry{raw: line}
	if f := strings.Split(line, ":"); len(f) == 7 {
		e.parsed = true
		e.uid, e.gid, e.comment, e.home, e.shell = f[2], f[3], f[4], f[5], f[6]
	}
	return e, true, nil
}

// lookupGroupGID resolves group — a name or a numeric GID, both of
// which `getent group` accepts, as real's group_info accepts both —
// to its GID. found is false when the group does not exist.
func lookupGroupGID(ctx context.Context, conn remoteexec.Connection, group string) (gid string, found bool, err error) {
	res, err := conn.Exec(ctx, "getent group "+shellQuote(group), nil)
	if err != nil {
		return "", false, fmt.Errorf("checking group %s: %w", group, err)
	}
	if res.RC != 0 {
		return "", false, nil
	}
	line, _, _ := strings.Cut(strings.TrimRight(res.Stdout, "\r\n"), "\n")
	f := strings.Split(line, ":")
	if len(f) < 3 || f[2] == "" {
		return "", false, fmt.Errorf("reading group %s: unexpected getent group output %q", group, line)
	}
	return f[2], true, nil
}

// argUID reads uid, which real declares type='int': an integer, or a
// string of one (YAML and --extra-vars both deliver either). Anything
// else is an argument error rather than a silently dropped -u — the
// silent drop is exactly the defect this argument was added to fix.
func argUID(args map[string]any) (int, bool, error) {
	v, ok := args["uid"]
	if !ok || v == nil {
		return 0, false, nil
	}
	switch n := v.(type) {
	case int:
		return n, true, nil
	case int64:
		return int(n), true, nil
	case float64:
		if n == math.Trunc(n) {
			return int(n), true, nil
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return parsed, true, nil
		}
	}
	return 0, false, errArg("argument uid must be an integer, got %v", v)
}
