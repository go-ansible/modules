package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePacmanKey implements (a subset of) Ansible's `pacman_key`
// module: adds or removes a GPG key from pacman's keyring via
// `pacman-key`.
//
// Args: id (string, required) — the key's 40-character identifier;
// data (string) — literal PGP public key block content; file (string)
// — a path on the target holding the key; keyserver (string) — fetch
// the key from this keyserver; keyring (string, default
// "/etc/pacman.d/gnupg"); ensure_trusted (bool, default false) — also
// locally signs the key via `pacman-key --lsign-key`; state (present|
// absent, default "present").
//
// Simplifications vs real pacman_key: no `url` (fetching a keyfile
// over HTTP — compose `curl | data` yourself via the data path if
// needed), `force_update`, or `verify` support. Idempotency for
// state=present is checked via `pacman-key --list-keys <id>` (exit 0
// iff already in the keyring, mirroring apt_key.go's own
// substring/exit-code idempotency style, but using pacman-key's exact
// exit code rather than a text grep since --list-keys already exits
// non-zero for an unknown key ID).
func modulePacmanKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	id, err := requireString(args, "id")
	if err != nil {
		return Result{}, err
	}
	keyring := argString(args, "keyring", "/etc/pacman.d/gnupg")
	state := argString(args, "state", "present")
	keyringFlag := " --gpgdir " + shellQuote(keyring)

	present, err := pacmanKeyPresent(ctx, conn, keyringFlag, id)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "absent":
		if !present {
			return Ok("key not present"), nil
		}
		if _, err := run(ctx, conn, "pacman-key"+keyringFlag+" --delete "+shellQuote(id)); err != nil {
			return Result{}, err
		}
		return Changed("removed key " + id), nil

	case "present":
		changed := false
		if !present {
			data := argString(args, "data", "")
			file := argString(args, "file", "")
			keyserver := argString(args, "keyserver", "")
			switch {
			case data != "":
				res, err := conn.Exec(ctx, "pacman-key"+keyringFlag+" --add -", strings.NewReader(data))
				if err != nil {
					return Result{}, err
				}
				if res.RC != 0 {
					return Fail("pacman-key --add: " + strings.TrimSpace(res.Stderr)), nil
				}
			case file != "":
				if _, err := run(ctx, conn, "pacman-key"+keyringFlag+" --add "+shellQuote(file)); err != nil {
					return Result{}, err
				}
			case keyserver != "":
				if _, err := run(ctx, conn, "pacman-key"+keyringFlag+" --keyserver "+shellQuote(keyserver)+" --recv-keys "+shellQuote(id)); err != nil {
					return Result{}, err
				}
			default:
				return Result{}, errArg("pacman_key: one of data, file, or keyserver is required to add a key")
			}
			changed = true
		}
		if argBool(args, "ensure_trusted", false) {
			if _, err := run(ctx, conn, "pacman-key"+keyringFlag+" --lsign-key "+shellQuote(id)); err != nil {
				return Result{}, err
			}
			changed = true
		}
		if !changed {
			return Ok("key already present"), nil
		}
		return Changed("key " + id), nil

	default:
		return Result{}, errArg("pacman_key: state must be present or absent, got %q", state)
	}
}

func pacmanKeyPresent(ctx context.Context, conn remoteexec.Connection, keyringFlag, id string) (bool, error) {
	res, err := runStatus(ctx, conn, "pacman-key"+keyringFlag+" --list-keys "+shellQuote(id)+" >/dev/null 2>&1")
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
