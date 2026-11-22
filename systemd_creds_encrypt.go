package modules

import (
	"context"
	"crypto/subtle"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSystemdCredsEncrypt implements Ansible's `systemd_creds_encrypt`
// (community.general) module: encrypts a secret via `systemd-creds
// encrypt`.
//
// Args (upstream, names and meaning as community.general 10.2.0):
// secret (required) — piped to `systemd-creds encrypt` via
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
// # Extensions (NOT in community.general)
//
// community.general's module cannot choose the key: systemd's default
// `auto` silently uses the host key on a machine without a TPM. These
// arguments are go-ansible extensions, each a thin pass-through to the
// systemd-creds(1) option of the same name (systemd version that added
// the option in parentheses, from the man page):
//
// with_key (--with-key=, v250) — one of host, tpm2, host+tpm2, null, auto,
// auto-initrd. Anything else, including the undocumented legacy alias
// tpm2-absent, is refused. tpm2_device (--tpm2-device=, v250) — an
// absolute device path on the target, or "auto". tpm2_pcrs
// (--tpm2-pcrs=, v250) — "+" separated PCR indexes 0..23; an empty
// string binds to no PCR. tpm2_public_key (--tpm2-public-key=, v252) — an
// absolute path of a PEM RSA public key on the target.
// tpm2_public_key_pcrs (--tpm2-public-key-pcrs=, v252) — as tpm2_pcrs;
// systemd defaults to 11. tpm2_signature (--tpm2-signature=, v252) — an
// absolute path of a systemd-measure signature file on the target; it is a
// DECRYPT option, so here it is used only by the decrypt that dest's
// idempotence check runs, and is refused without dest. Every path is on
// the target, must be absolute, and may contain only letters, digits and
// . _ / @ + : = , - ; no .. segment. Anything a shell could interpret is
// refused before any command runs, on top of shellQuote.
//
// tpm2_device_key is NOT implemented: neither the systemd-creds man page
// nor src/creds/creds.c (systemd main) has such an option.
//
// require_tpm (bool, default false) — fail unless the target has full
// TPM2 support, judged by `systemd-creds has-tpm2` (exit status 0 AND
// first line "yes"; "partial" and "no" fail, naming what is missing from
// the exit bits 1 firmware, 2 driver, 4 userspace; output that is none of
// yes, partial, no is a failure too, since the verb did not answer). With
// with_key unset or auto it also pins --with-key=host+tpm2, which is what
// auto picks on a typical machine (the man page: "bound to both") but a
// hard error instead of a silent host-only or null-key fallback when the
// TPM is unusable (inside a container, say). With with_key host, null or
// auto-initrd it is a contradiction and is refused. The module never falls
// back to the host key on its own.
//
// dest (path on the target, optional) — write the encrypted value there.
// The write is atomic: a 0600 temp file (mktemp, same directory) is
// filled, then renamed over dest. A symlink at dest, or a directory, is
// refused, never followed. The file belongs to whoever the connection runs
// as (root under become) unless owner (user name or numeric uid) is given.
// pretty is refused with dest, because a SetCredentialEncrypted= line
// cannot be decrypted back.
//
// # Idempotence of dest
//
// The ciphertext differs on every run (fresh nonce), so it cannot be
// compared. If dest exists, it is decrypted ON THE TARGET with
// `systemd-creds decrypt` (same name, user, tpm2_device, tpm2_signature)
// and the result is compared in memory with secret. Equal: changed=false,
// nothing written, nothing encrypted. Different, undecryptable (another
// key, wrong name, expired not_after, corrupt) or absent: encrypt, write,
// changed=true. The comparison is on decrypted content only; with equal
// content dest's owner and the options it was encrypted with are not
// corrected. The plaintext is never returned or logged. Without dest the
// module is unchanged from upstream: always changed=false. In check
// mode (the module is in checkModeSupport) it reports the change and
// writes nothing.
//
// Extra["value"]: the encrypted secret, Base64-encoded by systemd-creds
// itself, matching real systemd_creds_encrypt's own `value` return (with
// dest, present only when something was written). Extra["dest"] with dest.
//
// The secret leaves this module only on the target's stdin to
// systemd-creds. It is not in argv, in any result field, nor in an error
// message (any echo of a tool's stderr has it replaced by ********). This
// package has no argument-masking layer, so the guarantee is the module's
// own: task-level no_log: true is still advisable.
//
// Changed: false without dest — matching real systemd_creds_encrypt's own
// documented check_mode attribute ("This action does not modify
// state"): encryption is a pure computation with no persisted state on
// the target for Ansible's changed/unchanged model to describe.
func moduleSystemdCredsEncrypt(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "systemd_creds_encrypt"
	secret, err := requireString(args, "secret")
	if err != nil {
		return Result{}, err
	}

	withKey := argString(args, "with_key", "")
	if withKey != "" {
		ok := false
		for _, k := range systemdCredsWithKeys {
			ok = ok || k == withKey
		}
		if !ok {
			return Result{}, errArg("%s: with_key must be one of %s (got %q)", mod, strings.Join(systemdCredsWithKeys, ", "), withKey)
		}
	}
	requireTPM := argBool(args, "require_tpm", false)
	effectiveKey := withKey
	checkTPM := false
	if requireTPM {
		switch withKey {
		case "host", "null", "auto-initrd":
			return Result{}, errArg("%s: require_tpm contradicts with_key=%s, which does not use the TPM2", mod, withKey)
		case "", "auto":
			effectiveKey = "host+tpm2"
		}
		checkTPM = true
	}

	dest := argString(args, "dest", "")
	owner := argString(args, "owner", "")
	if dest != "" {
		if err := systemdCredsPath(mod, "dest", dest); err != nil {
			return Result{}, err
		}
		if strings.HasSuffix(dest, "/") {
			return Result{}, errArg("%s: dest must name a file, not a directory (got %q)", mod, dest)
		}
		if argBool(args, "pretty", false) {
			return Result{}, errArg("%s: pretty cannot be combined with dest: a SetCredentialEncrypted= line is not a credential file", mod)
		}
		if owner != "" && !systemdCredsOwnerRe.MatchString(owner) {
			return Result{}, errArg("%s: owner must be a user name or a numeric uid (got %q)", mod, owner)
		}
	} else {
		if owner != "" {
			return Result{}, errArg("%s: owner needs dest", mod)
		}
		if argString(args, "tpm2_signature", "") != "" {
			return Result{}, errArg("%s: tpm2_signature is a decrypt option and is only used with dest", mod)
		}
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
	if effectiveKey != "" {
		cmdArgs = append(cmdArgs, "--with-key="+effectiveKey)
	}
	if d := argString(args, "tpm2_device", ""); d != "" {
		if err := systemdCredsTPM2Device(mod, d); err != nil {
			return Result{}, err
		}
		cmdArgs = append(cmdArgs, "--tpm2-device="+d)
	}
	if argPresent(args, "tpm2_pcrs") {
		p := argString(args, "tpm2_pcrs", "")
		if p != "" {
			if err := systemdCredsPCRs(mod, "tpm2_pcrs", p); err != nil {
				return Result{}, err
			}
		}
		cmdArgs = append(cmdArgs, "--tpm2-pcrs="+p)
	}
	if k := argString(args, "tpm2_public_key", ""); k != "" {
		if err := systemdCredsPath(mod, "tpm2_public_key", k); err != nil {
			return Result{}, err
		}
		cmdArgs = append(cmdArgs, "--tpm2-public-key="+k)
	}
	if p := argString(args, "tpm2_public_key_pcrs", ""); p != "" {
		if err := systemdCredsPCRs(mod, "tpm2_public_key_pcrs", p); err != nil {
			return Result{}, err
		}
		cmdArgs = append(cmdArgs, "--tpm2-public-key-pcrs="+p)
	}
	cmdArgs = append(cmdArgs, "-", "-")

	// Validate the decrypt half too, before anything runs.
	var decryptFlags []string
	if dest != "" {
		if decryptFlags, err = systemdCredsDecryptFlags(mod, args); err != nil {
			return Result{}, err
		}
	}

	if checkTPM {
		why, err := systemdCredsRequireTPM2(ctx, conn, mod)
		if err != nil {
			return Result{}, err
		}
		if why != "" {
			return Fail(why), nil
		}
	}

	if dest != "" {
		same, res, err := systemdCredsDestUnchanged(ctx, conn, mod, args, dest, secret, decryptFlags)
		if err != nil || res != nil {
			if res != nil {
				return *res, nil
			}
			return Result{}, err
		}
		if same {
			return Ok(dest).WithExtra("dest", dest), nil
		}
		if InCheckMode(args) {
			return Changed(dest).WithExtra("dest", dest), nil
		}
	}

	res, err := conn.Exec(ctx, systemdCredsJoin(cmdArgs), strings.NewReader(secret))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(systemdCredsScrub("systemd_creds_encrypt: "+strings.TrimSpace(res.Stderr), secret)), nil
	}
	value := strings.TrimRight(res.Stdout, "\n")
	if dest == "" {
		return Ok("").WithExtra("value", value), nil
	}

	wr, err := conn.Exec(ctx, systemdCredsAtomicWrite(dest, owner), strings.NewReader(value+"\n"))
	if err != nil {
		return Result{}, err
	}
	if wr.RC != 0 {
		return Fail("systemd_creds_encrypt: writing " + dest + ": " + strings.TrimSpace(wr.Stderr)), nil
	}
	return Changed(dest).WithExtra("dest", dest).WithExtra("value", value), nil
}

// systemdCredsDestUnchanged reports whether dest already holds a credential
// that decrypts to secret. A non-nil *Result is a refusal (symlink,
// directory) to return as is. The decrypted bytes live only in this
// function's locals and are compared in constant time.
func systemdCredsDestUnchanged(ctx context.Context, conn remoteexec.Connection, mod string, args map[string]any, dest, secret string, decryptFlags []string) (bool, *Result, error) {
	if r, err := conn.Exec(ctx, "test -L "+shellQuote(dest), nil); err != nil {
		return false, nil, err
	} else if r.RC == 0 {
		f := Fail(mod + ": dest " + dest + " is a symlink; refusing to follow it")
		return false, &f, nil
	}
	if r, err := conn.Exec(ctx, "test -d "+shellQuote(dest), nil); err != nil {
		return false, nil, err
	} else if r.RC == 0 {
		f := Fail(mod + ": dest " + dest + " is a directory")
		return false, &f, nil
	}
	if r, err := conn.Exec(ctx, "test -e "+shellQuote(dest), nil); err != nil {
		return false, nil, err
	} else if r.RC != 0 {
		return false, nil, nil
	}

	// --name= is always passed (empty when unset), as upstream's decrypt
	// does: with a file as input systemd would otherwise derive the name
	// from the file name.
	dec := []string{"systemd-creds", "decrypt", "--name=" + argString(args, "name", "")}
	if u := argString(args, "user", ""); u != "" {
		dec = append(dec, "--uid="+u)
	}
	dec = append(dec, decryptFlags...)
	dec = append(dec, dest, "-")
	r, err := conn.Exec(ctx, systemdCredsJoin(dec), nil)
	if err != nil {
		return false, nil, err
	}
	if r.RC != 0 {
		return false, nil, nil
	}
	return subtle.ConstantTimeCompare([]byte(r.Stdout), []byte(secret)) == 1, nil, nil
}

// systemdCredsAtomicWrite is the shell script that stores stdin at dest:
// mktemp creates a 0600 file in dest's own directory (so the rename stays
// on one filesystem), it is filled, chmod'd 0600 again, optionally
// chown'd, and renamed over dest. mv replaces a symlink at dest rather
// than following it, and the temp file is removed on any failure.
func systemdCredsAtomicWrite(dest, owner string) string {
	dir, base := systemdCredsDestParts(dest)
	tmpl := dir + "/." + base + ".XXXXXX"
	if dir == "/" {
		tmpl = "/." + base + ".XXXXXX"
	}
	script := "umask 077; t=$(mktemp -- " + shellQuote(tmpl) + ") || exit 1; " +
		"trap 'rm -f -- \"$t\"' EXIT; " +
		"cat > \"$t\" && chmod 0600 -- \"$t\""
	if owner != "" {
		script += " && chown -- " + shellQuote(owner) + " \"$t\""
	}
	script += " && mv -f -- \"$t\" " + shellQuote(dest) + " && trap - EXIT"
	return script
}
