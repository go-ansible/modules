package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLdapEntry implements (a subset of) Ansible's `ldap_entry`
// module: asserts the existence (or non-existence) of one LDAP entry —
// its attributes are never modified once it exists (use ldap_attrs for
// that). See ldap_common.go's own doc comment for this port's shared
// "shells out to real ldapsearch/ldapadd/ldapdelete instead of linking
// python-ldap" architecture and its connection-argument mapping.
//
// Args: dn (string, required); state (present|absent, default
// "present"); objectClass (list of string, required when state=present
// — real ldap_entry enforces this the same way via required_if);
// attributes (dict, default {}) — extra attributes to set when
// CREATING the entry (never used to update an already-existing one —
// matching real ldap_entry's own documented "existing entries are
// never modified"); binary_attributes (list of string, default []) and
// honor_binary (bool, default false) — same Base64-decode-before-send
// semantics as ldap_attrs, see ldap_common.go's ldapIsBinaryAttr/
// ldapNormalizeAttrValues; recursive (bool, default false) — only
// meaningful for state=absent, mapped straight onto ldapdelete's own
// `-r` flag (recursive delete) rather than replicating real
// _delete_recursive's own "subtree-delete control, falling back to a
// manual reverse-sorted per-entry delete on NOT_ALLOWED_ON_NONLEAF"
// dance — ldapdelete -r already does exactly this branch-delete job
// server-side (per `man ldapdelete`), so there is nothing this port's
// own Go code needs to add.
//
// Existence is checked with a base-scope `ldapsearch ... -b <dn> -s
// base "(objectClass=*)" 1.1` (1.1 asks for no attributes back — this
// port only needs the exit code); a nonzero exit (LDAP "no such
// object") means absent. state=present with no existing entry sends
// one `ldapadd` LDIF add-record built from objectClass plus attributes
// (objectClass always wins over any "objectClass" key also present in
// attributes, matching real LdapEntry.__init__'s own unconditional
// `self.module.params["attributes"]["objectClass"] = ...` overwrite).
// state=absent with an existing entry runs `ldapdelete` (with `-r` if
// recursive).
//
// Like every ldap_* module here, xorder_discovery is applied to dn
// once up front (ldapResolveDN) and the resolved DN is used for both
// the existence check and the add/delete itself — matching real
// LdapGeneric.__init__'s own "resolve self.dn once, before dispatching
// to add()/delete()" order of operations exactly.
func moduleLdapEntry(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dn, err := requireString(args, "dn")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("ldap_entry: state must be present or absent, got %q", state)
	}
	recursive := argBool(args, "recursive", false)

	var objectClasses []string
	if state == "present" {
		objectClasses = argStringList(args, "objectClass")
		if len(objectClasses) == 0 {
			return Result{}, errArg("ldap_entry: objectClass is required when state is present")
		}
	}
	attributes, _ := args["attributes"].(map[string]any)
	binarySet := map[string]bool{}
	for _, a := range argStringList(args, "binary_attributes") {
		binarySet[strings.ToLower(a)] = true
	}
	honorBinary := argBool(args, "honor_binary", false)

	c, err := parseLdapConn(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := ldapRequireBinary(ctx, conn, "ldap_entry", "ldapsearch"); !ok {
		return res, nil
	}
	if state == "present" {
		if res, ok := ldapRequireBinary(ctx, conn, "ldap_entry", "ldapadd"); !ok {
			return res, nil
		}
	} else {
		if res, ok := ldapRequireBinary(ctx, conn, "ldap_entry", "ldapdelete"); !ok {
			return res, nil
		}
	}

	flags, cleanup, err := ldapAuthFlags(ctx, conn, c)
	defer cleanup()
	if err != nil {
		return Result{}, err
	}

	resolvedDN := ldapResolveDN(ctx, conn, c, flags, dn)

	existCmd := c.cmd("ldapsearch", flags, "-LLL", "-b", shellQuote(resolvedDN), "-s", "base", shellQuote("(objectClass=*)"), "1.1")
	existRes, err := runStatus(ctx, conn, existCmd)
	if err != nil {
		return Result{}, err
	}
	present := existRes.RC == 0

	if state == "absent" {
		if !present {
			return Ok(resolvedDN + " already absent"), nil
		}
		delFlags := append([]string{}, flags...)
		if recursive {
			delFlags = append(delFlags, "-r")
		}
		cmd := c.cmd("ldapdelete", delFlags, shellQuote(resolvedDN))
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Changed(resolvedDN + " removed"), nil
	}

	// state == "present"
	if present {
		return Ok(resolvedDN + " already present"), nil
	}

	full := map[string]any{}
	for k, v := range attributes {
		full[k] = v
	}
	ocValues := make([]any, len(objectClasses))
	for i, oc := range objectClasses {
		ocValues[i] = oc
	}
	full["objectClass"] = ocValues

	var b strings.Builder
	b.WriteString(ldifAttrLine("dn", []byte(resolvedDN)) + "\n")
	b.WriteString("changetype: add\n")
	var badAttrs []string
	for _, name := range sortedKeys(full) {
		isBinary := ldapIsBinaryAttr(name, honorBinary, binarySet)
		values, err := ldapNormalizeAttrValues(full[name], isBinary, false)
		if err != nil {
			badAttrs = append(badAttrs, name)
			continue
		}
		for _, v := range values {
			b.WriteString(ldifAttrLine(name, v) + "\n")
		}
	}
	if len(badAttrs) > 0 {
		return Fail("ldap_entry: invalid Base64-encoded attribute values for " + strings.Join(badAttrs, ", ")), nil
	}

	cmd := c.cmd("ldapadd", flags)
	res, err := conn.Exec(ctx, cmd, strings.NewReader(b.String()))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("ldap_entry: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Changed(resolvedDN + " added"), nil
}
