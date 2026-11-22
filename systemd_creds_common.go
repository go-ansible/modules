package modules

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// systemdCredsWithKeys is the complete value set systemd-creds(1) documents
// for --with-key= on `encrypt`: host, tpm2, host+tpm2, null, auto,
// auto-initrd (the special value "help" only lists them, so it is not a
// key). The legacy spelling tpm2-absent exists in src/creds/creds.c as an
// alias of null but is not documented in the man page, so it is refused.
var systemdCredsWithKeys = []string{"host", "tpm2", "host+tpm2", "null", "auto", "auto-initrd"}

var (
	// systemdCredsPathRe is the allowlist for a path handed to systemd-creds
	// (device node, PEM key, signature file, dest): absolute, and only
	// characters that are inert in a shell. Anything else is refused outright
	// instead of being quoted, so a hostile value never reaches the shell at
	// all (shellQuote stays as a second line of defence).
	systemdCredsPathRe = regexp.MustCompile(`^/[A-Za-z0-9._/@+:=,-]*$`)
	// systemdCredsPCRRe is the "+" separated numeric list of
	// systemd-creds(1): PCR indexes in the range 0..23.
	systemdCredsPCRRe = regexp.MustCompile(`^[0-9]{1,2}(\+[0-9]{1,2})*$`)
	// systemdCredsOwnerRe is a user name or a numeric uid.
	systemdCredsOwnerRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*\$?|[0-9]+)$`)
	ansiSGRRe           = regexp.MustCompile("\x1b\\[[0-9;]*m")
)

// systemdCredsPath validates a path argument. The path is on the TARGET.
func systemdCredsPath(module, key, v string) error {
	if !systemdCredsPathRe.MatchString(v) {
		return errArg("%s: %s must be an absolute path made only of letters, digits and . _ / @ + : = , - (got %q)", module, key, v)
	}
	for _, seg := range strings.Split(v, "/") {
		if seg == ".." {
			return errArg("%s: %s must not contain a .. segment (got %q)", module, key, v)
		}
	}
	return nil
}

// systemdCredsPCRs validates a PCR list such as "7" or "0+7+11". The empty
// string is valid for --tpm2-pcrs= (bind to no PCR at all) and is checked
// by the caller, not here.
func systemdCredsPCRs(module, key, v string) error {
	if !systemdCredsPCRRe.MatchString(v) {
		return errArg("%s: %s must be a + separated list of PCR indexes 0..23, e.g. 7+11 (got %q)", module, key, v)
	}
	seen := map[int]bool{}
	for _, f := range strings.Split(v, "+") {
		n, _ := strconv.Atoi(f)
		if n > 23 {
			return errArg("%s: %s: PCR index %d is out of range 0..23", module, key, n)
		}
		if seen[n] {
			return errArg("%s: %s lists PCR %d twice", module, key, n)
		}
		seen[n] = true
	}
	return nil
}

func argPresent(args map[string]any, key string) bool {
	_, ok := args[key]
	return ok
}

// systemdCredsTPM2Device validates tpm2_device: a device node path on the
// target, or the special value "auto". The special value "list" is not a
// device, it enumerates devices and prints them, so it is refused.
func systemdCredsTPM2Device(module, v string) error {
	if v == "auto" {
		return nil
	}
	return systemdCredsPath(module, "tpm2_device", v)
}

// systemdCredsDecryptFlags is the TPM2 part shared by every
// `systemd-creds decrypt` this package runs: the device and the PCR
// signature are the two options decrypt reads (the key is recorded inside
// the credential).
func systemdCredsDecryptFlags(module string, args map[string]any) ([]string, error) {
	var flags []string
	if d := argString(args, "tpm2_device", ""); d != "" {
		if err := systemdCredsTPM2Device(module, d); err != nil {
			return nil, err
		}
		flags = append(flags, "--tpm2-device="+d)
	}
	if s := argString(args, "tpm2_signature", ""); s != "" {
		if err := systemdCredsPath(module, "tpm2_signature", s); err != nil {
			return nil, err
		}
		flags = append(flags, "--tpm2-signature="+s)
	}
	return flags, nil
}

func systemdCredsJoin(cmdArgs []string) string {
	quoted := make([]string, len(cmdArgs))
	for i, a := range cmdArgs {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// systemdCredsScrub removes the secret from a message. This package has no
// no_log value scrubber (see telegram.go), so a module that echoes anything
// a tool printed must do the substitution itself.
func systemdCredsScrub(msg, secret string) string {
	if secret == "" {
		return msg
	}
	return strings.ReplaceAll(msg, secret, "********")
}

// systemdCredsRequireTPM2 runs `systemd-creds has-tpm2` and returns an
// empty string when the target has full TPM2 support, otherwise the reason.
//
// What it reads, from systemd's source (src/shared/tpm2-util.c,
// verb_has_tpm2_generic) and the systemd-analyze(1) man page: the first
// line of stdout is "yes", "partial" or "no"; the exit status is 0 only
// for full support, otherwise the OR of 1 (firmware missing), 2 (driver
// missing) and 4 (userspace support missing). Both must agree: exit 0 AND
// "yes". Output that is none of the three words means the verb did not
// run (an older systemd, or no systemd-creds at all), which is an error
// of its own and is never read as "no TPM, carry on".
func systemdCredsRequireTPM2(ctx context.Context, conn remoteexec.Connection, module string) (string, error) {
	res, err := conn.Exec(ctx, "systemd-creds has-tpm2", nil)
	if err != nil {
		return "", err
	}
	first := ""
	if lines := strings.SplitN(ansiSGRRe.ReplaceAllString(res.Stdout, ""), "\n", 2); len(lines) > 0 {
		first = strings.TrimSpace(lines[0])
	}
	switch first {
	case "yes", "partial", "no":
	default:
		return fmt.Sprintf("%s: cannot determine whether the target has a TPM2 (systemd-creds has-tpm2 exit %d, stdout %q, stderr %q); refusing to fall back to another key",
			module, res.RC, first, strings.TrimSpace(res.Stderr)), nil
	}
	if res.RC == 0 && first == "yes" {
		return "", nil
	}
	var missing []string
	if res.RC&1 != 0 {
		missing = append(missing, "firmware")
	}
	if res.RC&2 != 0 {
		missing = append(missing, "driver")
	}
	if res.RC&4 != 0 {
		missing = append(missing, "userspace support")
	}
	why := "TPM2 support is " + first
	if len(missing) > 0 {
		why += ", missing: " + strings.Join(missing, ", ")
	}
	return fmt.Sprintf("%s: require_tpm is set and the target has no usable TPM2 (%s); refusing to fall back to the host key", module, why), nil
}

// systemdCredsDestParts splits a validated dest into directory and base.
func systemdCredsDestParts(dest string) (dir, base string) {
	return path.Dir(dest), path.Base(dest)
}
