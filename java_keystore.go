package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJavaKeystore implements (a subset of) Ansible's `java_keystore`
// (community.general) module: bundles a certificate and private key
// into a Java keystore via `openssl pkcs12 -export` followed by
// `keytool -importkeystore` — read from real java_keystore.py's own
// JavaKeystore class (this batch's hard rule: the exact fingerprint-
// comparison idempotency check, the keystore-type magic-byte sniff,
// and the stdin-line-count conventions for both openssl and keytool
// are only visible there, not EXAMPLES/RETURN VALUES).
//
// Args: name (string, required) — the certificate's alias in the
// keystore; dest (string, required); password (string, required);
// exactly one of certificate/certificate_path is required (certificate
// content is written to a target-side temp file via conn.TempPath,
// mirroring java_cert.go's own inline-content handling — real
// java_keystore's own certificate/private_key content instead comes
// from the CONTROLLER's own filesystem, since real Ansible modules
// execute ON the target after being copied there; this port's
// Connection-only architecture means inline content is written to a
// target-side temp file instead, functionally equivalent for
// keytool/openssl's own purposes); likewise exactly one of
// private_key/private_key_path, plus private_key_passphrase (optional);
// force (bool, default false) — always regenerates the keystore;
// keystore_type (jks|pkcs12, optional); mode (octal string, optional)
// — this port supports ONLY `mode` from real java_keystore's own
// extends_documentation_fragment: ansible.builtin.files (owner/group/
// SELinux context/attributes/unsafe_writes are not implemented, same
// narrowing java_cert.go and ssh_config.go already document for their
// own written files in this package); ssl_backend (openssl|
// cryptography, default "openssl") — this port implements ONLY the
// openssl backend (shelling out to the target's own `openssl`/
// `keytool`, matching this package's general stance of using existing
// external tools rather than reimplementing a file format — see
// java_cert.go's own doc comment for the same stance); ssl_backend=
// cryptography returns Result{Failed:true} rather than silently
// falling back to openssl, since the two backends' own certificate/
// key-loading error behavior genuinely differs and this port has no
// way to honor a caller's explicit "use the cryptography library"
// request.
//
// Idempotency mirrors real JavaKeystore.cert_changed/
// read_stored_certificate_fingerprint: if dest doesn't exist yet, or
// force is true, the keystore is (re)created unconditionally.
// Otherwise, this port runs `keytool -list -alias <name> -keystore
// <dest> -v` (password piped via stdin, no -storepass flag — matching
// real java_keystore's own `data=self.password` invocation) and
// compares its own "SHA256: <fingerprint>" line against a freshly
// computed `openssl x509 -fingerprint -sha256` of the given
// certificate; any difference (including the alias not existing yet,
// or the given password not unlocking the existing keystore — both
// surfaced by real java_keystore as sentinel "always different"
// values, reproduced here as an unconditional "must recreate" rather
// than literal sentinel strings) triggers a full regeneration. If
// keystore_type is given and does not match the EXISTING file's own
// type (sniffed via its first 4 bytes: 0xFEEDFEED marks a JKS file,
// anything else is treated as PKCS12, matching real java_keystore's
// own current_type), that also forces regeneration.
//
// Regeneration always runs `openssl pkcs12 -export -name <name> -in
// <cert> -inkey <key> -out <tmp> -passout stdin` (+ `-passin stdin`
// when private_key_passphrase is given), with the export password(s)
// piped over stdin — never as a command-line argument (unlike
// java_cert.go's own -storepass flag convention; this module instead
// follows real java_keystore's own stdin-piping, which is the safer
// choice against process-listing exposure and is what upstream itself
// does for this specific password). If keystore_type=="pkcs12", that
// PKCS12 file IS the final keystore and is moved into place directly
// (no keytool step, matching real java_keystore's own `if
// self.keystore_type == "pkcs12": self.module.atomic_move(...)`
// shortcut); otherwise it's imported via `keytool -importkeystore`
// (adding `-deststoretype jks` when keystore_type=="jks"), with the
// keystore password(s) also piped over stdin (three lines, matching
// real java_keystore's own `data=f"{password}\n{password}\n{password}"`
// — accounting for keytool's own destination-password-entry-plus-
// confirmation and source-password prompts when neither
// -deststorepass nor -srcstorepass is given as a flag). If dest
// already exists at this point, it is removed before the keytool
// import (keytool doesn't cleanly regenerate an existing destination
// keystore from scratch otherwise); unlike real java_keystore's own
// backup-then-restore-on-failure safety net around that removal
// (`self.module.preserved_copy` before, restored if the import then
// fails), this port does NOT keep a rollback copy — a failed import
// after that point leaves dest simply absent rather than restored to
// its previous content. This is a genuine narrowing versus upstream,
// documented rather than silently accepted: a target relying on real
// java_keystore's own crash-safety here should be aware this port
// does not provide it.
//
// Simplifications vs real java_keystore: no owner/group/SELinux
// context/attributes/unsafe_writes (mode only, see above); no
// `cryptography` backend (see above); no keytool-help probing for
// `-deststoretype` support before adding it (real java_keystore checks
// `keytool -importkeystore -help`'s own output first; this port always
// adds the flag when keystore_type=="jks", which every keytool version
// in practical use today accepts); no backup/restore safety net around
// an existing dest during regeneration (see above); the `cmd`/`err`
// return values real java_keystore documents are populated only on
// failure, not success, matching its own RETURN VALUES ("returned:
// changed and failure" / "returned: failure").
func moduleJavaKeystore(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	dest, err := requireString(args, "dest")
	if err != nil {
		return Result{}, err
	}
	password, err := requireString(args, "password")
	if err != nil {
		return Result{}, err
	}
	force := argBool(args, "force", false)
	keystoreType := argString(args, "keystore_type", "")
	if keystoreType != "" && keystoreType != "jks" && keystoreType != "pkcs12" {
		return Result{}, errArg("java_keystore: keystore_type must be jks or pkcs12, got %q", keystoreType)
	}
	sslBackend := argString(args, "ssl_backend", "openssl")
	if sslBackend != "openssl" && sslBackend != "cryptography" {
		return Result{}, errArg("java_keystore: ssl_backend must be openssl or cryptography, got %q", sslBackend)
	}
	if sslBackend == "cryptography" {
		return Fail("java_keystore: ssl_backend=cryptography is not supported by this port (openssl only — see moduleJavaKeystore's own doc comment)"), nil
	}
	mode, err := argMode(args, "mode")
	if err != nil {
		return Result{}, err
	}

	cert := argString(args, "certificate", "")
	certPath := argString(args, "certificate_path", "")
	if (cert == "") == (certPath == "") {
		return Result{}, errArg("java_keystore: exactly one of certificate or certificate_path is required")
	}
	privateKey := argString(args, "private_key", "")
	privateKeyPath := argString(args, "private_key_path", "")
	if (privateKey == "") == (privateKeyPath == "") {
		return Result{}, errArg("java_keystore: exactly one of private_key or private_key_path is required")
	}
	passphrase := argString(args, "private_key_passphrase", "")

	if _, err := run(ctx, conn, "command -v openssl && command -v keytool"); err != nil {
		return Fail("java_keystore: openssl/keytool not found on the target"), nil
	}

	var cleanup []string
	defer func() {
		for _, p := range cleanup {
			_ = conn.Remove(ctx, p)
		}
	}()

	if certPath == "" {
		certPath = conn.TempPath("java_keystore-" + name + "-cert.pem")
		if _, err := conn.Exec(ctx, "cat > "+shellQuote(certPath), strings.NewReader(cert)); err != nil {
			return Result{}, err
		}
		cleanup = append(cleanup, certPath)
	}
	if privateKeyPath == "" {
		privateKeyPath = conn.TempPath("java_keystore-" + name + "-key.pem")
		if _, err := conn.Exec(ctx, "cat > "+shellQuote(privateKeyPath), strings.NewReader(privateKey)); err != nil {
			return Result{}, err
		}
		cleanup = append(cleanup, privateKeyPath)
	}

	exists, err := pathExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}

	mustCreate := force || !exists
	if !mustCreate {
		changed, ferr, err := javaKeystoreCertChanged(ctx, conn, dest, name, password, certPath, keystoreType)
		if err != nil {
			return Result{}, err
		}
		if ferr != "" {
			return Fail("java_keystore: " + ferr), nil
		}
		mustCreate = changed
	}

	changed := false
	if mustCreate {
		if ferr, err := javaKeystoreCreate(ctx, conn, dest, name, password, certPath, privateKeyPath, passphrase, keystoreType); err != nil {
			return Result{}, err
		} else if ferr != "" {
			return Fail("java_keystore: " + ferr), nil
		}
		changed = true
	}

	if mode != nil {
		before, err := statPath(ctx, conn, dest)
		if err != nil {
			return Result{}, err
		}
		if before == nil || before.mode != *mode {
			if _, err := run(ctx, conn, fmt.Sprintf("chmod %04o %s", *mode, shellQuote(dest))); err != nil {
				return Result{}, err
			}
			changed = true
		}
	}

	if !changed {
		return Ok(""), nil
	}
	return Changed(""), nil
}

// javaKeystoreCertChanged reports whether dest's existing keystore
// needs regenerating for certPath/password/keystoreType, per
// moduleJavaKeystore's own doc comment.
func javaKeystoreCertChanged(ctx context.Context, conn remoteexec.Connection, dest, name, password, certPath, keystoreType string) (changed bool, ferr string, err error) {
	if keystoreType != "" {
		actual, err := javaKeystoreCurrentType(ctx, conn, dest)
		if err != nil {
			return false, "", err
		}
		if actual != keystoreType {
			return true, "", nil
		}
	}

	res, err := conn.Exec(ctx, "keytool -list -alias "+shellQuote(name)+" -keystore "+shellQuote(dest)+" -v",
		strings.NewReader(password+"\n"))
	if err != nil {
		return false, "", err
	}
	if res.RC != 0 {
		out := res.Stdout
		if strings.Contains(out, "does not exist") {
			return true, "", nil // alias mismatch -> must recreate
		}
		if strings.Contains(strings.ToLower(out), "password was incorrect") {
			return true, "", nil // password mismatch -> must recreate
		}
		return false, strings.TrimSpace(res.Stderr + " " + out), nil
	}

	m := javaKeystoreSHA256Re.FindStringSubmatch(res.Stdout)
	if m == nil {
		return false, "unable to find the stored certificate fingerprint in keytool output", nil
	}
	stored := m[1]

	current, ferr, err := javaKeystoreCertFingerprint(ctx, conn, certPath)
	if err != nil {
		return false, "", err
	}
	if ferr != "" {
		return false, ferr, nil
	}
	return current != stored, "", nil
}

var javaKeystoreSHA256Re = regexp.MustCompile(`SHA256: ([0-9A-Fa-f:]+)`)
var javaKeystoreFingerprintRe = regexp.MustCompile(`=([0-9A-Fa-f:]+)`)

// javaKeystoreCertFingerprint runs `openssl x509 -fingerprint -sha256`
// on certPath and returns its fingerprint.
func javaKeystoreCertFingerprint(ctx context.Context, conn remoteexec.Connection, certPath string) (fingerprint, ferr string, err error) {
	res, err := runStatus(ctx, conn, "openssl x509 -noout -in "+shellQuote(certPath)+" -fingerprint -sha256")
	if err != nil {
		return "", "", err
	}
	if res.RC != 0 {
		return "", strings.TrimSpace(res.Stderr), nil
	}
	m := javaKeystoreFingerprintRe.FindStringSubmatch(res.Stdout)
	if m == nil {
		return "", "unable to find the current certificate fingerprint in openssl output", nil
	}
	return m[1], "", nil
}

// javaKeystoreCurrentType sniffs dest's first 4 bytes to distinguish a
// JKS file (magic 0xFEEDFEED) from PKCS12 (anything else), matching
// real java_keystore's own current_type.
func javaKeystoreCurrentType(ctx context.Context, conn remoteexec.Connection, dest string) (string, error) {
	out, err := run(ctx, conn, "od -An -tx1 -N4 "+shellQuote(dest))
	if err != nil {
		return "", err
	}
	if strings.ReplaceAll(strings.TrimSpace(out), " ", "") == "feedfeed" {
		return "jks", nil
	}
	return "pkcs12", nil
}

// javaKeystoreCreate regenerates dest from scratch: an `openssl pkcs12
// -export` bundle, then either moved directly into place
// (keystoreType=="pkcs12") or imported via `keytool -importkeystore`.
// A non-empty ferr mirrors real java_keystore's own module.fail_json
// wording for the failing step.
func javaKeystoreCreate(ctx context.Context, conn remoteexec.Connection, dest, name, password, certPath, privateKeyPath, passphrase, keystoreType string) (ferr string, err error) {
	p12tmp := conn.TempPath("java_keystore-" + name + ".p12")
	defer func() { _ = conn.Remove(ctx, p12tmp) }()

	exportCmd := "openssl pkcs12 -export -name " + shellQuote(name) +
		" -in " + shellQuote(certPath) + " -inkey " + shellQuote(privateKeyPath) +
		" -out " + shellQuote(p12tmp) + " -passout stdin"
	stdin := ""
	if passphrase != "" {
		exportCmd += " -passin stdin"
		stdin = passphrase + "\n"
	}
	stdin += password + "\n" + password

	res, err := conn.Exec(ctx, exportCmd, strings.NewReader(stdin))
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return strings.TrimSpace(res.Stderr), nil
	}

	if keystoreType == "pkcs12" {
		if _, err := run(ctx, conn, "mv "+shellQuote(p12tmp)+" "+shellQuote(dest)); err != nil {
			return "", err
		}
		return "", nil
	}

	exists, err := pathExists(ctx, conn, dest)
	if err != nil {
		return "", err
	}
	if exists {
		if _, err := run(ctx, conn, "rm -f "+shellQuote(dest)); err != nil {
			return "", err
		}
	}

	importCmd := "keytool -importkeystore -destkeystore " + shellQuote(dest) +
		" -srckeystore " + shellQuote(p12tmp) + " -srcstoretype pkcs12" +
		" -alias " + shellQuote(name) + " -noprompt"
	if keystoreType == "jks" {
		importCmd += " -deststoretype jks"
	}
	importStdin := password + "\n" + password + "\n" + password

	res, err = conn.Exec(ctx, importCmd, strings.NewReader(importStdin))
	if err != nil {
		return "", err
	}
	stillExists, err := pathExists(ctx, conn, dest)
	if err != nil {
		return "", err
	}
	if res.RC != 0 || !stillExists {
		return strings.TrimSpace(res.Stderr), nil
	}
	return "", nil
}
