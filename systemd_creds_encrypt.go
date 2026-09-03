package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSystemdCredsEncrypt implements Ansible's `systemd_creds_encrypt`
// (community.general) module: encrypts a secret via `systemd-creds
// encrypt`.
//
// Args: secret (required) — piped to `systemd-creds encrypt` via
// Connection.Exec's own stdin parameter rather than embedded in the
// shell command line, keeping it out of the target's process listing
// the same way consul_kv.go's own consulKv keeps a token out of argv via
// CONSUL_HTTP_TOKEN; name — embedded credential name
// (`--name=<name>`); not_after — `--not-after=<ts>`; timestamp —
// `--timestamp=<ts>`; user — `--uid=<user>` (a name, numeric UID, or the
// literal "self"; real systemd_creds_encrypt documents this as
// requiring systemd 256+, a version constraint this port has no way to
// check and does not attempt to); pretty (bool, default false) —
// `--pretty`/`-p`, pretty-printed for pasting into a unit file.
// `systemd-creds encrypt` is invoked as `systemd-creds encrypt [flags]
// - -` (both INPUT and OUTPUT positional arguments as "-", meaning
// stdin/stdout).
//
// Extra["value"]: the encrypted secret, Base64-encoded by systemd-creds
// itself, matching real systemd_creds_encrypt's own `value` return.
//
// Changed: always false — matching real systemd_creds_encrypt's own
// documented check_mode attribute ("This action does not modify
// state"): encryption is a pure computation with no persisted state on
// the target for Ansible's changed/unchanged model to describe.
func moduleSystemdCredsEncrypt(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	secret, err := requireString(args, "secret")
	if err != nil {
		return Result{}, err
	}

	cmdArgs := []string{"systemd-creds", "encrypt"}
	if n := argString(args, "name", ""); n != "" {
		cmdArgs = append(cmdArgs, "--name="+n)
	}
	if na := argString(args, "not_after", ""); na != "" {
		cmdArgs = append(cmdArgs, "--not-after="+na)
	}
	if ts := argString(args, "timestamp", ""); ts != "" {
		cmdArgs = append(cmdArgs, "--timestamp="+ts)
	}
	if u := argString(args, "user", ""); u != "" {
		cmdArgs = append(cmdArgs, "--uid="+u)
	}
	if argBool(args, "pretty", false) {
		cmdArgs = append(cmdArgs, "--pretty")
	}
	cmdArgs = append(cmdArgs, "-", "-")

	quoted := make([]string, len(cmdArgs))
	for i, a := range cmdArgs {
		quoted[i] = shellQuote(a)
	}
	res, err := conn.Exec(ctx, strings.Join(quoted, " "), strings.NewReader(secret))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("systemd_creds_encrypt: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Ok("").WithExtra("value", strings.TrimRight(res.Stdout, "\n")), nil
}
