package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleJavaCert implements (a subset of) Ansible's `java_cert` module:
// a wrapper around the target's own `keytool` binary that imports a
// certificate (or a PKCS12 keystore's certificate+private key) into a
// Java keystore, or removes an entry from one by alias.
//
// Matching the stance unarchive.go and htpasswd.go's own doc comments
// already take for their external-tool dependencies: this module hard-
// requires `keytool` (from a JRE/JDK) on the target — reimplementing
// Java's own keystore file format in Go was judged out of scope and,
// unlike a hashing algorithm, has no simple well-known spec to target —
// and fails cleanly via a Result{Failed:true} if `command -v
// <executable>` comes up empty, rather than silently no-op-ing.
//
// Args: cert_alias (string, required by this port — see below);
// keystore_path (string, required); keystore_pass (string, required);
// keystore_type (string, optional — passed as keytool's own
// -storetype/-deststoretype flag); keystore_create (bool, default
// false) — this port pre-checks keystore_path's existence in Go and
// fails early if it's missing and keystore_create is false; if true (or
// the file already exists) the import proceeds, relying on keytool's
// own real behavior of transparently creating a new keystore file on
// its first -importcert/-importkeystore call, so keystore_create=true
// does not need its own separate "create empty keystore" command; state
// (present|absent, default "present"); trust_cacert (bool, default
// false) — adds keytool's -trustcacerts flag; executable (string,
// default "keytool").
//
// Exactly one certificate source is required when state=present,
// matching real java_cert's own documented constraint:
//   - cert_path (string) — a certificate file already present ON THE
//     TARGET (like every other bare "path" argument in this port's
//     modules — e.g. sysctl_file, htpasswd's path — this is NOT
//     uploaded from the control node the way copy.go's `src` is; real
//     java_cert's own module has no remote_src-style option either,
//     since it executes on the target and just opens the path there).
//   - cert_content (string) — inline PEM certificate text; written to a
//     target-side temp file via conn.TempPath+conn.Exec (stdin), then
//     imported exactly like cert_path, then removed (best-effort,
//     mirroring script.go/unarchive.go's own cleanup pattern).
//   - cert_url (string, paired with cert_port, default 443) — fetches
//     the live TLS endpoint's leaf certificate via `openssl s_client
//     -connect <url>:<port> -servername <url> </dev/null 2>/dev/null |
//     sed -ne '/-BEGIN CERTIFICATE-/,/-END CERTIFICATE-/p'`, redirected
//     into a target-side temp file, then imported and removed the same
//     way as cert_content. This mirrors real java_cert's own actual
//     fetch technique (openssl s_client against the target URL); it
//     makes `openssl` a second hard external dependency for this one
//     code path specifically, checked with its own `command -v openssl`
//     gate before the fetch is attempted.
//   - pkcs12_path (string, path, already on the target — same
//     no-upload rule as cert_path) — imported via keytool's own
//     `-importkeystore` (PKCS12 -> the destination keystore's type),
//     which is exactly what real java_cert does for this source too.
//     pkcs12_alias (string) selects the source entry; if omitted, this
//     port defaults it to cert_alias (the common case is a single-entry
//     PKCS12 file created for exactly this import, e.g. via `openssl
//     pkcs12 -export`, using the same alias on both sides) rather than
//     plain keytool's own default of importing every entry in the
//     store — a deliberate narrowing, since importing "every entry"
//     has no single alias to report presence/absence for.
//     pkcs12_password (string, default "") is passed as -srcstorepass;
//     no fallback to keystore_pass is assumed if omitted.
//
// Idempotency for state=present is checked via `keytool -list -alias
// <cert_alias> -keystore <keystore_path> -storepass <keystore_pass>`'s
// exit code alone (per this batch's task spec) — if the alias is
// already present at all, this port reports "unchanged" and does NOT
// re-import. This is a deliberate simplification versus real
// java_cert's own documented behavior ("When state=present, the
// certificate is always inserted into the keystore, even if there
// already exists a cert alias that is different") — real java_cert
// always overwrites; this port's alias-presence check means a
// certificate that has changed under an unchanged alias will NOT be
// detected as needing re-import. The same tradeoff (existence check
// instead of exact content/fingerprint comparison) is what apt_key.go
// already makes for its own idempotency check, for the same reason:
// avoiding a full compare of a tool's own non-trivial output format.
//
// Simplifications vs real java_cert: cert_alias is required here (real
// java_cert allows omitting it, letting keytool assign its own default
// alias "mykey" — but this port's idempotency and state=absent removal
// are both defined entirely in terms of a known alias, so there is no
// way to manage an unaliased entry); no owner/group/mode/attributes/
// SELinux context/unsafe_writes support on keystore_path (this port
// never chowns/chmods a file it writes, see blockinfile.go's own
// simplifications list); no diff_mode, and the `cmd` return value real
// java_cert documents is not populated in Extra.
func moduleJavaCert(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	alias, err := requireString(args, "cert_alias")
	if err != nil {
		return Result{}, err
	}
	keystorePath, err := requireString(args, "keystore_path")
	if err != nil {
		return Result{}, err
	}
	keystorePass, err := requireString(args, "keystore_pass")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("java_cert: state must be present or absent, got %q", state)
	}
	executable := argString(args, "executable", "keytool")

	if _, err := run(ctx, conn, "command -v "+shellQuote(executable)); err != nil {
		return Fail(fmt.Sprintf("java_cert: %s not found on the target (this module requires a JRE/JDK's "+
			"keytool on the target — see moduleJavaCert's doc comment)", executable)), nil
	}

	present, err := javaCertAliasPresent(ctx, conn, executable, keystorePath, keystorePass, alias)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !present {
			return Ok(alias + " not present in " + keystorePath), nil
		}
		cmd := shellQuote(executable) + " -delete -noprompt -alias " + shellQuote(alias) +
			" -keystore " + shellQuote(keystorePath) + " -storepass " + shellQuote(keystorePass)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(alias + " removed from " + keystorePath), nil
	}

	if present {
		return Ok(alias + " already present in " + keystorePath), nil
	}

	keystoreCreate := argBool(args, "keystore_create", false)
	exists, err := pathExists(ctx, conn, keystorePath)
	if err != nil {
		return Result{}, err
	}
	if !exists && !keystoreCreate {
		return Fail(fmt.Sprintf("java_cert: keystore %s does not exist and keystore_create is false", keystorePath)), nil
	}

	keystoreType := argString(args, "keystore_type", "")
	trustCacert := argBool(args, "trust_cacert", false)

	pkcs12Path := argString(args, "pkcs12_path", "")
	certPath := argString(args, "cert_path", "")
	certContent := argString(args, "cert_content", "")
	certURL := argString(args, "cert_url", "")

	sources := 0
	for _, s := range []string{pkcs12Path, certPath, certContent, certURL} {
		if s != "" {
			sources++
		}
	}
	if sources != 1 {
		return Result{}, errArg("java_cert: exactly one of cert_url, cert_path, cert_content, or pkcs12_path is required")
	}

	switch {
	case pkcs12Path != "":
		pkcs12Alias := argString(args, "pkcs12_alias", alias)
		pkcs12Password := argString(args, "pkcs12_password", "")
		cmd := javaCertImportKeystoreCmd(executable, pkcs12Path, pkcs12Alias, pkcs12Password, alias, keystorePath, keystorePass, keystoreType)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}

	case certPath != "":
		cmd := javaCertImportCertCmd(executable, certPath, alias, keystorePath, keystorePass, keystoreType, trustCacert)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}

	case certContent != "":
		tmp := conn.TempPath("java_cert-" + alias + ".pem")
		if _, err := conn.Exec(ctx, "cat > "+shellQuote(tmp), strings.NewReader(certContent)); err != nil {
			return Result{}, err
		}
		defer func() { _ = conn.Remove(ctx, tmp) }()
		cmd := javaCertImportCertCmd(executable, tmp, alias, keystorePath, keystorePass, keystoreType, trustCacert)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}

	case certURL != "":
		if _, err := run(ctx, conn, "command -v openssl"); err != nil {
			return Fail("java_cert: openssl not found on the target (required to fetch cert_url's live " +
				"certificate — see moduleJavaCert's doc comment)"), nil
		}
		certPort := argInt(args, "cert_port", 443)
		tmp := conn.TempPath("java_cert-" + alias + ".pem")
		fetchCmd := "openssl s_client -connect " + shellQuote(fmt.Sprintf("%s:%d", certURL, certPort)) +
			" -servername " + shellQuote(certURL) + " </dev/null 2>/dev/null | " +
			"sed -ne '/-BEGIN CERTIFICATE-/,/-END CERTIFICATE-/p' > " + shellQuote(tmp)
		if _, err := run(ctx, conn, fetchCmd); err != nil {
			return Result{}, err
		}
		defer func() { _ = conn.Remove(ctx, tmp) }()
		cmd := javaCertImportCertCmd(executable, tmp, alias, keystorePath, keystorePass, keystoreType, trustCacert)
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
	}

	return Changed(alias + " imported into " + keystorePath), nil
}

// javaCertAliasPresent reports whether alias already exists in
// keystorePath, via `keytool -list -alias`'s exit code (see
// moduleJavaCert's doc comment on why this is weaker than real
// java_cert's own always-reinsert behavior).
func javaCertAliasPresent(ctx context.Context, conn remoteexec.Connection, executable, keystorePath, keystorePass, alias string) (bool, error) {
	cmd := shellQuote(executable) + " -list -alias " + shellQuote(alias) +
		" -keystore " + shellQuote(keystorePath) + " -storepass " + shellQuote(keystorePass) + " >/dev/null 2>&1"
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}

// javaCertImportCertCmd builds the `keytool -importcert` invocation
// shared by the cert_path, cert_content, and cert_url sources (they all
// reduce to "a PEM file that's now somewhere on the target").
func javaCertImportCertCmd(executable, certFile, alias, keystorePath, keystorePass, keystoreType string, trustCacert bool) string {
	cmd := shellQuote(executable) + " -importcert -noprompt -alias " + shellQuote(alias) +
		" -file " + shellQuote(certFile) +
		" -keystore " + shellQuote(keystorePath) +
		" -storepass " + shellQuote(keystorePass)
	if keystoreType != "" {
		cmd += " -storetype " + shellQuote(keystoreType)
	}
	if trustCacert {
		cmd += " -trustcacerts"
	}
	return cmd
}

// javaCertImportKeystoreCmd builds the `keytool -importkeystore`
// invocation used for the pkcs12_path source, converting a PKCS12
// keystore's alias into the destination Java keystore.
func javaCertImportKeystoreCmd(executable, pkcs12Path, pkcs12Alias, pkcs12Password, alias, keystorePath, keystorePass, keystoreType string) string {
	cmd := shellQuote(executable) + " -importkeystore -noprompt" +
		" -srckeystore " + shellQuote(pkcs12Path) +
		" -srcstoretype PKCS12" +
		" -srcstorepass " + shellQuote(pkcs12Password) +
		" -srcalias " + shellQuote(pkcs12Alias) +
		" -destkeystore " + shellQuote(keystorePath) +
		" -deststorepass " + shellQuote(keystorePass) +
		" -destalias " + shellQuote(alias)
	if keystoreType != "" {
		cmd += " -deststoretype " + shellQuote(keystoreType)
	}
	return cmd
}
