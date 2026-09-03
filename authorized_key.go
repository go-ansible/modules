package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAuthorizedKey implements (a subset of) Ansible's
// `authorized_key` module: adds or removes entries in a user's
// `~/.ssh/authorized_keys` file — very similar in shape to
// known_hosts.go's marked-entry management.
//
// Args: user (string, required) — resolved to a home directory via
// `getent passwd`, matching user.go's own portable lookup convention;
// key (string, required) — one or more public keys, one per line (real
// authorized_key also accepts a keys.github.com/gitlab URL or a
// `file://` prefix for controller-side lookup; this port only accepts
// literal key material — no HTTP fetch, no validate_certs, matching
// this package's "no network client behavior baked into a module"
// convention already used elsewhere); state (present|absent, default
// "present"); exclusive (bool, default false) — when true and
// state=present, the file's content is replaced with EXACTLY the given
// keys, removing every other line; key_options (string, optional) —
// prepended to each key line; manage_dir (bool, default true) —
// `mkdir -p` the .ssh directory (mode 0700) if missing; path (string,
// optional) — overrides the computed `<home>/.ssh/authorized_keys`.
//
// Simplifications vs real authorized_key: no `comment` rewriting (a
// changed comment is not detected or applied — this port only ever
// appends/removes whole lines), no `follow` (symlink handling), no
// directory/file ownership or mode management beyond manage_dir's
// mkdir (real authorized_key also chowns the directory/file to `user`
// and sets its mode; this port does not, since it has no portable,
// privilege-safe way to do so from an arbitrary connection user).
// Idempotency for state=present is an exact full-line match on the
// composed "<key_options> <key>" text: changing key_options for an
// already-present key appends a new line rather than rewriting the
// old one in place (the old, options-less variant is left behind as a
// redundant duplicate) — a real gap versus real authorized_key's
// in-place option rewriting, documented rather than silently claimed.
func moduleAuthorizedKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	user, err := requireString(args, "user")
	if err != nil {
		return Result{}, err
	}
	keyArg, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("authorized_key: state must be present or absent, got %q", state)
	}
	exclusive := argBool(args, "exclusive", false)
	keyOptions := argString(args, "key_options", "")
	manageDir := argBool(args, "manage_dir", true)

	path := argString(args, "path", "")
	if path == "" {
		home, err := userHomeDir(ctx, conn, user)
		if err != nil {
			return Result{}, err
		}
		path = home + "/.ssh/authorized_keys"
	}

	var lines []string
	for _, k := range splitLines(keyArg) {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if keyOptions != "" {
			k = keyOptions + " " + k
		}
		lines = append(lines, k)
	}
	if len(lines) == 0 {
		return Result{}, errArg("authorized_key: key must contain at least one non-empty line")
	}

	if manageDir {
		dir := shellDirname(path)
		if _, err := run(ctx, conn, "mkdir -p "+shellQuote(dir)+" && chmod 700 "+shellQuote(dir)); err != nil {
			return Result{}, err
		}
	}

	if state == "absent" {
		exists, err := pathExists(ctx, conn, path)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Ok(path + " does not exist"), nil
		}
		changed := false
		for _, line := range lines {
			present, err := knownHostLinePresent(ctx, conn, path, line)
			if err != nil {
				return Result{}, err
			}
			if !present {
				continue
			}
			cmd := "grep -vxF " + shellQuote(line) + " " + shellQuote(path) + " > " + shellQuote(path+".tmp") +
				" && mv " + shellQuote(path+".tmp") + " " + shellQuote(path)
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
			changed = true
		}
		if changed {
			return Changed(user + "'s authorized_keys updated"), nil
		}
		return Ok(user + "'s authorized_keys already absent"), nil
	}

	// state == "present"
	if exclusive {
		want := strings.Join(lines, "\n") + "\n"
		current, err := fetchIfExists(ctx, conn, path)
		if err == nil && current != nil && string(current) == want {
			return Ok(user + "'s authorized_keys already exclusive"), nil
		}
		if err != nil {
			return Result{}, err
		}
		if _, err := conn.Exec(ctx, "cat > "+shellQuote(path), strings.NewReader(want)); err != nil {
			return Result{}, err
		}
		return Changed(user + "'s authorized_keys replaced (exclusive)"), nil
	}

	changed := false
	for _, line := range lines {
		present, err := knownHostLinePresent(ctx, conn, path, line)
		if err != nil {
			return Result{}, err
		}
		if present {
			continue
		}
		cmd := "printf '%s\\n' " + shellQuote(line) + " >> " + shellQuote(path)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		changed = true
	}
	if changed {
		return Changed(user + "'s authorized_keys updated"), nil
	}
	return Ok(user + "'s authorized_keys already up to date"), nil
}

// userHomeDir resolves user's home directory via `getent passwd`,
// portably across distros (see user.go/getent.go's own use of getent
// for the same reason).
func userHomeDir(ctx context.Context, conn remoteexec.Connection, user string) (string, error) {
	out, err := run(ctx, conn, "getent passwd "+shellQuote(user))
	if err != nil {
		return "", errArg("authorized_key: could not resolve home directory for user %q: %v", user, err)
	}
	fields := strings.Split(out, ":")
	if len(fields) < 6 || fields[5] == "" {
		return "", errArg("authorized_key: unexpected getent passwd output for user %q", user)
	}
	return fields[5], nil
}
