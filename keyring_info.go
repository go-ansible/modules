package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleKeyringInfo implements Ansible's `keyring_info`
// (community.general) module: reads a secret from the OS keyring, via
// `secret-tool lookup` / `security find-generic-password -w` — see
// keyring.go's own doc comment for this port's narrowing of real
// keyring_info's cross-platform Python `keyring` library abstraction
// down to these two CLI-backed OSes, the keyringGet helper this module
// shares with keyring.go's own state=present idempotency check, and
// exactly what keyring_password does (and, on macOS, does not do)
// here.
//
// Args: service (required); username (required); keyring_password
// (required).
//
// Extra["passphrase"] is set only when the secret exists (matching
// real keyring_info's own "returned: success and the password exists"
// contract); when it doesn't, this port returns Ok with no
// Extra["passphrase"] key at all, matching real keyring_info.py's own
// `if passphrase is not None: result["passphrase"] = passphrase` (no
// key set, rather than an explicit null, when absent).
func moduleKeyringInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	service, err := requireString(args, "service")
	if err != nil {
		return Result{}, err
	}
	username, err := requireString(args, "username")
	if err != nil {
		return Result{}, err
	}
	if _, err := requireString(args, "keyring_password"); err != nil {
		return Result{}, err
	}
	keyringPassword := argString(args, "keyring_password", "")

	value, found, failRes, err := keyringGet(ctx, conn, service, username, keyringPassword)
	if err != nil {
		return Result{}, err
	}
	if failRes.Failed {
		return failRes, nil
	}
	label := service + "@" + username
	if !found {
		return Ok("Password for " + label + " does not exist."), nil
	}
	return Ok("Successfully retrieved password for "+label).WithExtra("passphrase", value), nil
}
