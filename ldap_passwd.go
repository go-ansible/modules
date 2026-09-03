package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLdapPasswd implements (a subset of) Ansible's `ldap_passwd`
// module: sets one LDAP entry's userPassword — this module only
// asserts that a given password is valid for a given entry, it does
// not assert the entry itself exists (see ldap_entry for that). See
// ldap_common.go's own doc comment for this port's shared "shells out
// to real ldap-utils tools instead of linking python-ldap" architecture
// and its connection-argument mapping.
//
// Args: dn (string, required); passwd (string, required by this port
// — real ldap_passwd's own argspec technically allows omitting it,
// defaulting to Python None, but binding/setting a None password has
// no well-defined real-world behavior worth replicating, so this port
// requires it outright, a deliberate narrowing).
//
// Idempotency mirrors real passwd_check exactly, just via a different
// mechanism: real ldap_passwd opens a SEPARATE, throwaway connection
// and attempts connection.simple_bind_s(dn, passwd) — if that bind
// succeeds, the password already matches (unchanged); if it fails with
// INVALID_CREDENTIALS, it doesn't (needs setting). This port does the
// same by shelling out to `ldapwhoami`, binding AS dn with passwd
// (never the module's own bind_dn/bind_pw — those, if given, identify
// who is allowed to CHANGE the password below, not whose password is
// being checked): passwd is written to a target-side temp file (never
// the command line, matching ldap_common.go's own "never put a secret
// in a command line" rule) and passed via ldapwhoami's own `-y` flag.
// Unlike real passwd_check, this port does not distinguish "bind
// failed because the password is wrong" from any other bind failure
// (a down server, a bad DN, ...) at this pre-check stage — any nonzero
// ldapwhoami exit is treated as "needs setting" and this port proceeds
// to the actual ldappasswd call below, which will itself fail loudly
// with a clear error if the real problem is something other than a
// stale password. This is a deliberate, documented simplification, not
// a silent behavior gap.
//
// Setting the password uses `ldappasswd`, which implements the same
// LDAPv3 Password Modify (RFC 3062) extended operation real
// ldap_passwd's own connection.passwd_s(dn, None, passwd) call does —
// authenticated as this module's own bind_dn/bind_pw/sasl_class (an
// admin identity with permission to change dn's password), targeting
// dn as the trailing positional argument, with the new password passed
// via ldappasswd's own `-T <file>` flag (again, a temp file, never the
// command line — the same passwd content already written for the
// ldapwhoami check above is reused for this).
func moduleLdapPasswd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dn, err := requireString(args, "dn")
	if err != nil {
		return Result{}, err
	}
	passwd, err := requireString(args, "passwd")
	if err != nil {
		return Result{}, err
	}

	c, err := parseLdapConn(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := ldapRequireBinary(ctx, conn, "ldap_passwd", "ldapwhoami"); !ok {
		return res, nil
	}
	if res, ok := ldapRequireBinary(ctx, conn, "ldap_passwd", "ldappasswd"); !ok {
		return res, nil
	}

	adminFlags, adminCleanup, err := ldapAuthFlags(ctx, conn, c)
	defer adminCleanup()
	if err != nil {
		return Result{}, err
	}

	resolvedDN := ldapResolveDN(ctx, conn, c, adminFlags, dn)

	pwPath, pwCleanup, err := ldapWriteTempFile(ctx, conn, "ldap-passwd", passwd)
	defer pwCleanup()
	if err != nil {
		return Result{}, err
	}

	checkFlags := []string{"-H", shellQuote(c.serverURI)}
	if c.startTLS {
		checkFlags = append(checkFlags, "-Z")
	}
	checkFlags = append(checkFlags, "-x", "-D", shellQuote(resolvedDN), "-y", shellQuote(pwPath))
	checkCmd := c.cmd("ldapwhoami", checkFlags)
	checkRes, err := runStatus(ctx, conn, checkCmd)
	if err != nil {
		return Result{}, err
	}
	if checkRes.RC == 0 {
		return Ok(resolvedDN + " password already set"), nil
	}

	setCmd := c.cmd("ldappasswd", adminFlags, "-T", shellQuote(pwPath), shellQuote(resolvedDN))
	setRes, err := runStatus(ctx, conn, setCmd)
	if err != nil {
		return Result{}, err
	}
	if setRes.RC != 0 {
		return Fail("ldap_passwd: " + strings.TrimSpace(setRes.Stderr)), nil
	}
	return Changed(resolvedDN + " password set"), nil
}
