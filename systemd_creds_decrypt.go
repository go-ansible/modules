package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSystemdCredsDecrypt implements Ansible's `systemd_creds_decrypt`
// (community.general) module: decrypts a secret via `systemd-creds
// decrypt`.
//
// Args: secret (required) — the encrypted (Base64) blob, piped to
// `systemd-creds decrypt` via Connection.Exec's own stdin parameter
// rather than embedded in the shell command line, matching
// systemd_creds_encrypt.go's own reasoning; name — validates the
// embedded credential name (`--name=<name>`); timestamp —
// `--timestamp=<ts>`, validates the "not-after" timestamp used during
// encryption; user — `--uid=<user>` (see systemd_creds_encrypt.go's own
// note on the "self"/systemd-256+ caveat this port cannot check);
// newline (bool, default false) — `--newline` (systemd-creds' own name
// for this flag is inferred: real systemd_creds_decrypt documents only
// the module-level behavior "add a trailing newline if not present",
// which this port maps to a matching real systemd-creds CLI flag by
// name rather than confirming it against systemd's own upstream man
// page, since only ansible-doc — not systemd's own docs — was
// available while implementing this batch); transcode
// (base64|unbase64|hex|unhex) — `--transcode=<t>`.
//
// # Extensions (NOT in community.general)
//
// tpm2_device (--tpm2-device=, systemd v250) — an absolute device path on
// the target, or "auto"; tpm2_signature (--tpm2-signature=, systemd v252) —
// an absolute path on the target of a systemd-measure PCR signature file,
// needed to decrypt a credential sealed with tpm2_public_key. Paths are
// validated as in systemd_creds_encrypt.go. with_key and the other tpm2_*
// options are encrypt-only: the man page says the key is recorded in the
// credential. Neither is in the generated command line unless given.
//
// `systemd-creds decrypt` is invoked as `systemd-creds decrypt [flags]
// - -` (both INPUT and OUTPUT positional arguments as "-", meaning
// stdin/stdout).
//
// Extra["value"]: the decrypted secret. Real systemd_creds_decrypt's own
// doc notes Ansible only supports UTF-8 strings — a binary or
// differently-encoded secret needs transcode=base64/hex to round-trip
// safely; this port makes no attempt to detect or reject invalid UTF-8
// itself, simply returning whatever `systemd-creds decrypt` printed.
//
// Changed: always false, matching systemd_creds_encrypt.go's own
// "This action does not modify state" reasoning.
func moduleSystemdCredsDecrypt(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	secret, err := requireString(args, "secret")
	if err != nil {
		return Result{}, err
	}

	cmdArgs := []string{"systemd-creds", "decrypt"}
	if n := argString(args, "name", ""); n != "" {
		cmdArgs = append(cmdArgs, "--name="+n)
	}
	if ts := argString(args, "timestamp", ""); ts != "" {
		cmdArgs = append(cmdArgs, "--timestamp="+ts)
	}
	if u := argString(args, "user", ""); u != "" {
		cmdArgs = append(cmdArgs, "--uid="+u)
	}
	if argBool(args, "newline", false) {
		cmdArgs = append(cmdArgs, "--newline")
	}
	if tc := argString(args, "transcode", ""); tc != "" {
		cmdArgs = append(cmdArgs, "--transcode="+tc)
	}
	tpmFlags, err := systemdCredsDecryptFlags("systemd_creds_decrypt", args)
	if err != nil {
		return Result{}, err
	}
	cmdArgs = append(cmdArgs, tpmFlags...)
	cmdArgs = append(cmdArgs, "-", "-")

	res, err := conn.Exec(ctx, systemdCredsJoin(cmdArgs), strings.NewReader(secret))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(systemdCredsScrub("systemd_creds_decrypt: "+strings.TrimSpace(res.Stderr), secret)), nil
	}
	return Ok("").WithExtra("value", res.Stdout), nil
}
