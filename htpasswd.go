package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHtpasswd implements (a subset of) Ansible's `htpasswd` module:
// adds or removes a username/password entry in an Apache/Nginx-style
// htpasswd file, entirely by shelling out to the target's own
// `htpasswd` binary.
//
// Real community.general.htpasswd is implemented against Python's
// `passlib' library directly — it never calls the `htpasswd' command
// line tool at all, which is how it supports the wide range of hash
// schemes passlib itself implements. This port has no passlib and no
// Python runtime on the target to lean on, and — matching the stance
// unarchive.go and java_cert.go's doc comments already take for their
// own external-tool dependencies — deliberately does NOT attempt to
// reimplement bcrypt/apr_md5_crypt/crypt(3) password hashing in pure
// Go: getting a password hash format bit-for-bit compatible with what
// Apache/Nginx's own basic-auth parsers expect is exactly the kind of
// security-sensitive, easy-to-get-subtly-wrong code this project treats
// requiring a real target-side dependency as the honest, valued choice
// over. So this module hard-requires the target's own `htpasswd`
// executable (from apache2-utils/httpd-tools) and fails cleanly via a
// Result{Failed:true} — not a Go error — if `command -v htpasswd` comes
// up empty, rather than silently no-op-ing or trying to fake a hash.
//
// Args: path (string, required, aliased from dest/destfile in real
// htpasswd); name (string, required, aliased from username); password
// (string, required to create a new entry; may be omitted when the
// entry already exists and state=present, in which case this port
// reports "unchanged" without being able to verify the stored password
// still matches anything — since there is nothing to compare against);
// state (present|absent, default "present"); create (bool, default
// true) — matches real htpasswd's own "create the file if it does not
// exist, else fail" semantics exactly, since that logic lives in this
// port's own Go code (checking existence before ever invoking the
// binary), not in the `htpasswd` tool itself; hash_scheme (string,
// aliased from crypt_scheme, default "apr_md5_crypt") — mapped to the
// real htpasswd binary's own hashing flags: "apr_md5_crypt" -> -m,
// "des_crypt" -> -d, "ldap_sha1" -> -s, "plaintext" -> -p, "bcrypt" ->
// -B. Any other value real htpasswd (the module) would accept via
// passlib — "md5_crypt", "sha256_crypt", "portable_apache22",
// "host_apache24", and anything else passlib supports beyond the five
// above — is rejected with a clear error, since the `htpasswd` binary
// itself has no flag for them.
//
// Idempotency for state=present is checked via `htpasswd -vb <path>
// <name> <password>` — the binary's own verify mode — so, exactly like
// real htpasswd's own documented limitation ("The module has no
// mechanism to determine the hash_scheme of an existing entry"), a
// password that already matches is reported unchanged regardless of
// which hash_scheme produced the stored hash; changing hash_scheme
// alone (same password) is not detected as a change either, matching
// real htpasswd's own behavior exactly (its docs recommend removing
// and re-adding the entry to change scheme, which applies equally
// here). Presence for state=absent (and for the password-omitted
// present case above) is checked with a plain `awk -F:` match on the
// username field — not htpasswd's own -v, since that needs a password
// to test against — so a malformed line with extra colons in the
// password hash field could misparse; htpasswd-file hash fields don't
// contain literal ':' in any of this port's five supported schemes, so
// this is not expected to matter in practice.
//
// Simplifications vs real htpasswd: no owner/group/mode/attributes/
// SELinux context/unsafe_writes support (this port never chowns/chmods
// a file it writes — see blockinfile.go's own simplifications list for
// the same narrowing elsewhere in this package).
func moduleHtpasswd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path, err := requireString(args, "path")
	if err != nil {
		return Result{}, err
	}
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("htpasswd: state must be present or absent, got %q", state)
	}

	if _, err := run(ctx, conn, "command -v htpasswd"); err != nil {
		return Fail("htpasswd: the htpasswd binary is required on the target and was not found in PATH " +
			"(this port shells out to the real htpasswd tool rather than reimplementing its password " +
			"hashing in Go — see moduleHtpasswd's doc comment)"), nil
	}

	if state == "absent" {
		present, err := htpasswdUserPresent(ctx, conn, path, name)
		if err != nil {
			return Result{}, err
		}
		if !present {
			return Ok(name + " not present in " + path), nil
		}
		if _, err := run(ctx, conn, "htpasswd -D "+shellQuote(path)+" "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		return Changed(name + " removed from " + path), nil
	}

	create := argBool(args, "create", true)
	exists, err := pathExists(ctx, conn, path)
	if err != nil {
		return Result{}, err
	}
	if !exists && !create {
		return Fail(fmt.Sprintf("htpasswd: %s does not exist and create is false", path)), nil
	}

	hashScheme := argString(args, "hash_scheme", "apr_md5_crypt")
	schemeFlag, err := htpasswdSchemeFlag(hashScheme)
	if err != nil {
		return Result{}, err
	}

	password := argString(args, "password", "")
	if password == "" {
		if !exists {
			return Result{}, errArg("htpasswd: password is required to create a new entry for %q", name)
		}
		present, err := htpasswdUserPresent(ctx, conn, path, name)
		if err != nil {
			return Result{}, err
		}
		if present {
			return Ok(name + " already present in " + path), nil
		}
		return Result{}, errArg("htpasswd: password is required to create a new entry for %q", name)
	}

	needChange := true
	if exists {
		res, err := runStatus(ctx, conn, "htpasswd -vb "+shellQuote(path)+" "+shellQuote(name)+" "+shellQuote(password)+" 2>/dev/null")
		if err != nil {
			return Result{}, err
		}
		needChange = res.RC != 0
	}
	if !needChange {
		return Ok(name + " already set in " + path), nil
	}

	flags := []string{"-b"}
	if !exists {
		flags = append(flags, "-c")
	}
	if schemeFlag != "" {
		flags = append(flags, schemeFlag)
	}
	cmd := "htpasswd " + strings.Join(flags, " ") + " " + shellQuote(path) + " " + shellQuote(name) + " " + shellQuote(password)
	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	if exists {
		return Changed(name + " updated in " + path), nil
	}
	return Changed(name + " added to " + path), nil
}

// htpasswdSchemeFlag maps the "hash_scheme" argument to the real
// htpasswd binary's own hashing flag (see moduleHtpasswd's doc comment
// for exactly which schemes this port supports and why).
func htpasswdSchemeFlag(scheme string) (string, error) {
	switch scheme {
	case "apr_md5_crypt":
		return "-m", nil
	case "des_crypt":
		return "-d", nil
	case "ldap_sha1":
		return "-s", nil
	case "plaintext":
		return "-p", nil
	case "bcrypt":
		return "-B", nil
	default:
		return "", errArg("htpasswd: hash_scheme %q is not supported by this port — only the schemes the "+
			"real htpasswd binary itself implements are available (apr_md5_crypt, des_crypt, ldap_sha1, "+
			"plaintext, bcrypt); real htpasswd's other passlib-backed schemes need passlib itself, which "+
			"this port does not shell out to", scheme)
	}
}

// htpasswdUserPresent reports whether path already has a line for name
// (matched on the username field before the first ':', via awk rather
// than htpasswd -v since presence-checking has no password to verify
// against). A nonexistent path is treated as "not present", not an
// error.
func htpasswdUserPresent(ctx context.Context, conn remoteexec.Connection, path, name string) (bool, error) {
	cmd := "awk -F: -v u=" + shellQuote(name) + " '$1==u{f=1} END{exit !f}' " + shellQuote(path) + " 2>/dev/null"
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
