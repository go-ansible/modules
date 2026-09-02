package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAptKey implements (a subset of) Ansible's `apt_key` module:
// adds or removes an APT signing key on a Debian/Ubuntu target via the
// classic `apt-key` command.
//
// Real ansible.builtin.apt_key is itself documented as deprecated
// upstream — Debian/Ubuntu's own apt tooling has deprecated apt-key in
// favor of keyring files under /etc/apt/keyrings/, and apt-key is slated
// for eventual removal. Real Ansible still ships apt_key for
// compatibility with existing playbooks; this port does the same,
// implementing the classic apt-key behavior rather than the newer
// keyring-file approach.
//
// Args: id (string) — the key's ID/fingerprint, used with keyserver to
// fetch a key, or alone with state=absent to remove one; keyserver
// (string, paired with id) — fetch the key from this keyserver; url
// (string) — fetch key data from this URL instead of a keyserver;
// state (present|absent, default "present").
//
// Simplifications vs real apt_key: no `data` (inline key material),
// `file` (local key file), `keyring` (alternate keyring path), or
// `validate_certs` support. Idempotency for state=present greps
// `apt-key list`'s human-readable output for id as a plain substring —
// weaker than real apt_key's exact fingerprint comparison, but it
// avoids parsing apt-key's multi-line output format, which varies by
// gnupg version.
func moduleAptKey(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	id := argString(args, "id", "")
	keyserver := argString(args, "keyserver", "")
	url := argString(args, "url", "")
	state := argString(args, "state", "present")

	if id == "" && url == "" {
		return Result{}, errArg("apt_key: at least one of id or url is required")
	}

	switch state {
	case "absent":
		if id == "" {
			return Result{}, errArg("apt_key: id is required when state is absent")
		}
		present, err := aptKeyPresent(ctx, conn, id)
		if err != nil {
			return Result{}, err
		}
		if !present {
			return Ok("key not present"), nil
		}
		if _, err := run(ctx, conn, "apt-key del "+shellQuote(id)); err != nil {
			return Result{}, err
		}
		return Changed("removed key " + id), nil

	case "present":
		if id != "" {
			present, err := aptKeyPresent(ctx, conn, id)
			if err != nil {
				return Result{}, err
			}
			if present {
				return Ok("key already present"), nil
			}
		}
		var cmd string
		switch {
		case url != "":
			cmd = "curl -fsSL " + shellQuote(url) + " | apt-key add -"
		case keyserver != "":
			cmd = "apt-key adv --keyserver " + shellQuote(keyserver) + " --recv-keys " + shellQuote(id)
		default:
			return Result{}, errArg("apt_key: id without keyserver or url has no way to fetch the key")
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed("added key"), nil

	default:
		return Result{}, errArg("apt_key: state must be present or absent, got %q", state)
	}
}

// aptKeyPresent reports whether id already appears in `apt-key list`'s
// output, a plain case-insensitive substring match (see the doc comment
// on moduleAptKey for why this is weaker than exact-fingerprint
// comparison).
func aptKeyPresent(ctx context.Context, conn remoteexec.Connection, id string) (bool, error) {
	res, err := conn.Exec(ctx, "apt-key list 2>/dev/null | grep -qi "+shellQuote(id), nil)
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
