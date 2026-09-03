package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLdapInc implements (a subset of) Ansible's `ldap_inc` module:
// atomically increments a numeric LDAP attribute and returns its new
// value — read from real ldap_inc.py's own LdapInc class (this batch's
// hard rule: the exact rootDSE-based method-autodetection and the
// legacy retry loop are only visible in the implementation, not
// EXAMPLES/OPTIONS). Extends ldap_common.go's shared helpers
// (parseLdapConn/ldapAuthFlags/ldapResolveDN/ldapRequireBinary/
// ldapCurrentValues/ldifAttrLine/c.cmd) rather than duplicating them —
// see ldap_common.go's own doc comment for this port's shared "shells
// out to real ldapsearch/ldapmodify instead of linking python-ldap"
// architecture and connection-argument mapping.
//
// Args: dn (string, required); attribute (string, required); increment
// (int, default 1); method (auto|rfc4525|legacy, default "auto"); plus
// every connection argument ldap_common.go's parseLdapConn accepts.
//
// Two methods, matching real LdapInc exactly:
//
//   - rfc4525: sends one `ldapmodify` LDIF using the "increment:"
//     mod-spec keyword RFC4525 defines (OpenLDAP's ldapmodify(1) has
//     supported this natively since 2.4.23 — a real LDIF verb, not
//     invented for this port). Real LdapInc additionally attaches an
//     LDAP Post-Read control to read the new value back in the SAME
//     round trip; this port cannot reproduce that specific optimization,
//     since ldap-utils' CLI tools do not expose a control response's
//     data on stdout in an easily machine-parseable form (documented
//     deviation, not a faked behavior) — instead, after a successful
//     ldapmodify, this port issues a separate ldapsearch read of
//     attribute's new value via ldap_common.go's own ldapCurrentValues.
//     The end state (the attribute incremented by exactly `increment`)
//     is identical either way; only the round-trip count differs.
//   - legacy: reads the current value, computes current+increment,
//     then sends one `ldapmodify` LDIF that deletes the old value and
//     adds the new one in a single changerecord — if that fails (a
//     concurrent modification raced this one), the whole read-compute-
//     modify sequence is retried, up to 3 attempts total, matching real
//     LdapInc's own tries/max_tries loop exactly.
//
// method="auto" probes the server's rootDSE (`ldapsearch -s base -b ""
// "(objectClass=*)" supportedControl supportedFeatures
// supportedExtension`) for the LDAP Post-Read control OID
// (1.3.6.1.1.13.2) AND the Modify-Increment feature/extension OID
// (1.3.6.1.1.14) — matching real main()'s own rootDSE check exactly;
// rfc4525 is used only when both are advertised, legacy otherwise
// (including when the rootDSE probe itself fails, an honest fallback
// to the more broadly compatible method rather than erroring).
//
// increment=0 is a read-only no-op (matching real main()'s own `if
// mod.increment != 0 ...  else: <read-only path>`): the current value
// is read and returned unchanged, incremented=false, no ldapmodify is
// ever sent. In every other case, a missing entry or an entry with no
// value for attribute is Fail("The entry does not exist or does not
// contain the specified attribute."), matching real code's own message
// text; a legacy method exhausting all 3 tries is
// Fail("The increment could not be applied after 3 tries.").
//
// Returns Extra["attribute"] (name), Extra["value"] (new value, as a
// decimal string, matching real RETURN's own `value: type: str`),
// Extra["incremented"] (bool), Extra["rfc4525"] (bool, which method was
// actually used — false for both increment=0 and the legacy path,
// matching real main()'s own default).
func moduleLdapInc(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dn, err := requireString(args, "dn")
	if err != nil {
		return Result{}, err
	}
	attribute, err := requireString(args, "attribute")
	if err != nil {
		return Result{}, err
	}
	increment := argInt(args, "increment", 1)
	method := argString(args, "method", "auto")
	if method != "auto" && method != "rfc4525" && method != "legacy" {
		return Result{}, errArg("ldap_inc: method must be auto, rfc4525, or legacy, got %q", method)
	}

	c, err := parseLdapConn(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := ldapRequireBinary(ctx, conn, "ldap_inc", "ldapsearch"); !ok {
		return res, nil
	}
	if res, ok := ldapRequireBinary(ctx, conn, "ldap_inc", "ldapmodify"); !ok {
		return res, nil
	}

	flags, cleanup, err := ldapAuthFlags(ctx, conn, c)
	defer cleanup()
	if err != nil {
		return Result{}, err
	}

	resolvedDN := ldapResolveDN(ctx, conn, c, flags, dn)

	if increment == 0 {
		current, err := ldapCurrentValues(ctx, conn, c, flags, resolvedDN, attribute)
		if err != nil {
			return Result{}, err
		}
		if len(current) == 0 {
			return Fail("ldap_inc: The entry does not exist or does not contain the specified attribute."), nil
		}
		return Ok("").
			WithExtra("attribute", attribute).
			WithExtra("value", current[0]).
			WithExtra("incremented", false).
			WithExtra("rfc4525", false), nil
	}

	rfc4525 := method == "rfc4525"
	if method == "auto" {
		rfc4525 = ldapSupportsModifyIncrement(ctx, conn, c, flags)
	}

	if rfc4525 {
		value, failMsg, err := ldapIncRFC4525(ctx, conn, c, flags, resolvedDN, attribute, increment)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail("ldap_inc: " + failMsg), nil
		}
		return Changed("").
			WithExtra("attribute", attribute).
			WithExtra("value", value).
			WithExtra("incremented", true).
			WithExtra("rfc4525", true), nil
	}

	value, failMsg, err := ldapIncLegacy(ctx, conn, c, flags, resolvedDN, attribute, increment)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail("ldap_inc: " + failMsg), nil
	}
	return Changed("").
		WithExtra("attribute", attribute).
		WithExtra("value", value).
		WithExtra("incremented", true).
		WithExtra("rfc4525", false), nil
}

// ldapSupportsModifyIncrement implements method="auto"'s own rootDSE
// probe — see moduleLdapInc's own doc comment for the exact two OIDs
// checked and the honest fallback-to-legacy behavior on a failed
// probe. This does NOT go through ldap_common.go's own parseLdif/
// ldapLdifEntry (which key entries by DN and drop any entry whose DN
// is empty — exactly what a base-scope, empty-base rootDSE search
// returns): it scans lines with parseLdifLine directly instead, a
// narrower reuse of ldap_common.go's own line-level parser that avoids
// that entry-boundary edge case rather than changing shared parsing
// behavior every other ldap_* module also depends on.
func ldapSupportsModifyIncrement(ctx context.Context, conn remoteexec.Connection, c ldapConn, flags []string) bool {
	const postReadOID = "1.3.6.1.1.13.2"
	const modifyIncrementOID = "1.3.6.1.1.14"

	cmd := c.cmd("ldapsearch", flags, "-LLL", "-o", "ldif-wrap=no", "-b", shellQuote(""), "-s", "base",
		shellQuote("(objectClass=*)"), "supportedControl", "supportedFeatures", "supportedExtension")
	res, err := runStatus(ctx, conn, cmd)
	if err != nil || res.RC != 0 {
		return false
	}

	hasControl := false
	hasFeatureOrExt := false
	for _, line := range strings.Split(res.Stdout, "\n") {
		attr, val, ok := parseLdifLine(strings.TrimRight(line, "\r"))
		if !ok {
			continue
		}
		switch strings.ToLower(attr) {
		case "supportedcontrol":
			if val == postReadOID {
				hasControl = true
			}
		case "supportedfeatures", "supportedextension":
			if val == modifyIncrementOID {
				hasFeatureOrExt = true
			}
		}
	}
	return hasControl && hasFeatureOrExt
}

// ldapIncRFC4525 sends the RFC4525 "increment:" LDIF changerecord and
// then re-reads attribute's new value (see moduleLdapInc's own doc
// comment for why this port cannot use the postread control's
// same-round-trip value). failMsg is set (err nil) for a well-formed
// ldapmodify failure (e.g. the server rejected "increment:" after all,
// despite the rootDSE advertising it, or method=rfc4525 was forced
// against a server that does not support it).
func ldapIncRFC4525(ctx context.Context, conn remoteexec.Connection, c ldapConn, flags []string, dn, attribute string, increment int) (value, failMsg string, err error) {
	var b strings.Builder
	b.WriteString(ldifAttrLine("dn", []byte(dn)) + "\n")
	b.WriteString("changetype: modify\n")
	b.WriteString("increment: " + attribute + "\n")
	b.WriteString(ldifAttrLine(attribute, []byte(strconv.Itoa(increment))) + "\n")
	b.WriteString("-\n")

	res, err := conn.Exec(ctx, c.cmd("ldapmodify", flags), strings.NewReader(b.String()))
	if err != nil {
		return "", "", err
	}
	if res.RC != 0 {
		return "", strings.TrimSpace(res.Stderr), nil
	}

	current, err := ldapCurrentValues(ctx, conn, c, flags, dn, attribute)
	if err != nil {
		return "", "", err
	}
	if len(current) == 0 {
		return "", "The entry does not exist or does not contain the specified attribute.", nil
	}
	return current[0], "", nil
}

// ldapIncLegacy implements the read-compute-delete/add retry loop —
// see moduleLdapInc's own doc comment for the exact tries/max_tries
// semantics this matches from real LdapInc.
func ldapIncLegacy(ctx context.Context, conn remoteexec.Connection, c ldapConn, flags []string, dn, attribute string, increment int) (value, failMsg string, err error) {
	const maxTries = 3
	for try := 0; try < maxTries; try++ {
		current, err := ldapCurrentValues(ctx, conn, c, flags, dn, attribute)
		if err != nil {
			return "", "", err
		}
		if len(current) == 0 {
			return "", "The entry does not exist or does not contain the specified attribute.", nil
		}
		curInt, convErr := strconv.Atoi(current[0])
		if convErr != nil {
			return "", "", errArg("ldap_inc: current value %q of %s is not an integer", current[0], attribute)
		}
		newVal := strconv.Itoa(curInt + increment)

		var b strings.Builder
		b.WriteString(ldifAttrLine("dn", []byte(dn)) + "\n")
		b.WriteString("changetype: modify\n")
		b.WriteString("delete: " + attribute + "\n")
		b.WriteString(ldifAttrLine(attribute, []byte(current[0])) + "\n")
		b.WriteString("-\n")
		b.WriteString("add: " + attribute + "\n")
		b.WriteString(ldifAttrLine(attribute, []byte(newVal)) + "\n")
		b.WriteString("-\n")

		res, err := conn.Exec(ctx, c.cmd("ldapmodify", flags), strings.NewReader(b.String()))
		if err != nil {
			return "", "", err
		}
		if res.RC == 0 {
			return newVal, "", nil
		}
		// A concurrent modification changed the value out from under
		// this delete/add pair — matching real code's own
		// `except ldap.NO_SUCH_ATTRIBUTE` retry, this port simply
		// retries on ANY ldapmodify failure here (this port cannot
		// distinguish "the old value we tried to delete is now stale"
		// from other ldapmodify failures via exit code alone; retrying
		// either way is harmless and converges to the same outcome).
	}
	return "", "The increment could not be applied after 3 tries.", nil
}
