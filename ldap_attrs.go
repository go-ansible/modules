package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLdapAttrs implements (a subset of) Ansible's `ldap_attrs`
// module: adds, removes, or forces-exact multiple attribute VALUES on
// an already-existing LDAP entry (never the entry's own existence —
// see ldap_entry for that). See ldap_common.go's own doc comment for
// this port's shared "shells out to real ldapsearch/ldapmodify instead
// of linking python-ldap" architecture and its connection-argument
// mapping.
//
// Args: dn (string, required); attributes (dict, required) — attribute
// name -> a single string or a list of strings; state (present|absent|
// exact, default "present"); binary_attributes (list of string,
// default []) and honor_binary (bool, default false) — same semantics
// as ldap_entry, see ldap_common.go's ldapIsBinaryAttr; ordered (bool,
// default false) — prepends fresh "{i}" X-ORDERed index numbers to
// each text (non-binary) attribute's list values, replacing any
// existing "{n}" prefix, matching real _order_values exactly (mainly
// useful for managing olcAccess-style ACL lists).
//
// For each attribute, this port fetches its CURRENT values with one
// base-scope `ldapsearch ... "(objectClass=*)" <attr>` per attribute,
// then computes the needed change entirely in Go (byte/string set
// comparison) before issuing a single combined `ldapmodify` LDIF
// changerecord covering every changed attribute in one call — matching
// real add()/delete()/exact()'s own "build one modlist, one
// connection.modify_s() call" shape, but NOT real add()/delete()'s own
// per-value comparison strategy: real ldap_attrs compares text-
// attribute values for state=present/absent ON THE SERVER (an actual
// LDAP filter search per value, honoring the attribute's own LDAP
// matching rules), falling back to a Python-side comparison only for
// state=exact or binary attributes (its own documented caveat: "it is
// theoretically possible to see spurious changes when target and
// actual values are semantically identical but lexically distinct").
// This port uses that same client-side comparison for ALL THREE
// states, present/absent included — an intentional, honest widening of
// real ldap_attrs's own already-documented limitation, not a new kind
// of bug: a value already present under a lexically-different-but-
// matching-rule-equivalent form (case folding, whitespace collapsing,
// DN syntax normalization, etc.) may be reported as a change here where
// real ldap_attrs would not.
//
// state=present adds any listed value not already present (a `add:`
// LDIF hunk); state=absent removes any listed value that is present (a
// `delete:` hunk); state=exact compares the full current value set
// against the requested set — if different, an empty current set uses
// `add:`, an empty requested set uses a valueless `delete:` (removes
// the whole attribute), and any other case uses `replace:`.
//
// Returns Extra["modlist"], a list of {"op", "attr", "values"} maps —
// a simpler, JSON-friendly shape than real ldap_attrs's own
// modlist return value (a list of raw (MOD_ADD/MOD_DELETE/MOD_REPLACE
// integer code, attr, values) tuples), a deliberate deviation
// documented here rather than replicating python-ldap's own integer
// opcodes into Go.
func moduleLdapAttrs(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dn, err := requireString(args, "dn")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "exact" {
		return Result{}, errArg("ldap_attrs: state must be present, absent, or exact, got %q", state)
	}
	attributes, ok := args["attributes"].(map[string]any)
	if !ok || len(attributes) == 0 {
		return Result{}, errArg("ldap_attrs: attributes is required")
	}
	binarySet := map[string]bool{}
	for _, a := range argStringList(args, "binary_attributes") {
		binarySet[strings.ToLower(a)] = true
	}
	honorBinary := argBool(args, "honor_binary", false)
	ordered := argBool(args, "ordered", false)

	c, err := parseLdapConn(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := ldapRequireBinary(ctx, conn, "ldap_attrs", "ldapsearch"); !ok {
		return res, nil
	}
	if res, ok := ldapRequireBinary(ctx, conn, "ldap_attrs", "ldapmodify"); !ok {
		return res, nil
	}

	flags, cleanup, err := ldapAuthFlags(ctx, conn, c)
	defer cleanup()
	if err != nil {
		return Result{}, err
	}

	resolvedDN := ldapResolveDN(ctx, conn, c, flags, dn)

	type hunk struct {
		op     string
		attr   string
		values [][]byte
	}
	var hunks []hunk
	var modlist []any
	var badAttrs []string

	for _, name := range sortedKeys(attributes) {
		isBinary := ldapIsBinaryAttr(name, honorBinary, binarySet)
		norm, err := ldapNormalizeAttrValues(attributes[name], isBinary, ordered)
		if err != nil {
			badAttrs = append(badAttrs, name)
			continue
		}
		current, err := ldapCurrentValues(ctx, conn, c, flags, resolvedDN, name)
		if err != nil {
			return Result{}, err
		}
		currentSet := map[string]bool{}
		for _, v := range current {
			currentSet[v] = true
		}

		switch state {
		case "present":
			var add [][]byte
			for _, v := range norm {
				if !currentSet[string(v)] {
					add = append(add, v)
				}
			}
			if len(add) > 0 {
				hunks = append(hunks, hunk{"add", name, add})
				modlist = append(modlist, ldapModlistEntry("add", name, add))
			}
		case "absent":
			var del [][]byte
			for _, v := range norm {
				if currentSet[string(v)] {
					del = append(del, v)
				}
			}
			if len(del) > 0 {
				hunks = append(hunks, hunk{"delete", name, del})
				modlist = append(modlist, ldapModlistEntry("delete", name, del))
			}
		case "exact":
			normSet := map[string]bool{}
			for _, v := range norm {
				normSet[string(v)] = true
			}
			same := len(normSet) == len(currentSet)
			if same {
				for v := range normSet {
					if !currentSet[v] {
						same = false
						break
					}
				}
			}
			if !same {
				switch {
				case len(current) == 0:
					hunks = append(hunks, hunk{"add", name, norm})
					modlist = append(modlist, ldapModlistEntry("add", name, norm))
				case len(norm) == 0:
					hunks = append(hunks, hunk{"delete", name, nil})
					modlist = append(modlist, ldapModlistEntry("delete", name, nil))
				default:
					hunks = append(hunks, hunk{"replace", name, norm})
					modlist = append(modlist, ldapModlistEntry("replace", name, norm))
				}
			}
		}
	}

	if len(badAttrs) > 0 {
		return Fail("ldap_attrs: invalid Base64-encoded attribute values for " + strings.Join(badAttrs, ", ")), nil
	}
	if len(hunks) == 0 {
		return Ok(resolvedDN+" attributes already "+state).WithExtra("modlist", modlist), nil
	}

	var b strings.Builder
	b.WriteString(ldifAttrLine("dn", []byte(resolvedDN)) + "\n")
	b.WriteString("changetype: modify\n")
	for _, h := range hunks {
		b.WriteString(h.op + ": " + h.attr + "\n")
		for _, v := range h.values {
			b.WriteString(ldifAttrLine(h.attr, v) + "\n")
		}
		b.WriteString("-\n")
	}

	cmd := c.cmd("ldapmodify", flags)
	res, err := conn.Exec(ctx, cmd, strings.NewReader(b.String()))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("ldap_attrs: " + strings.TrimSpace(res.Stderr)), nil
	}
	return Changed(resolvedDN+" attributes updated").WithExtra("modlist", modlist), nil
}

// ldapCurrentValues fetches attr's current values on dn via a
// base-scope search restricted to that one attribute. A missing entry,
// or an entry with no values for attr, both come back as an empty
// (nil) slice, not an error — this port treats "nothing to compare
// against" the same as "attribute absent" for present/absent/exact
// purposes.
func ldapCurrentValues(ctx context.Context, conn remoteexec.Connection, c ldapConn, flags []string, dn, attr string) ([]string, error) {
	cmd := c.cmd("ldapsearch", flags, "-LLL", "-o", "ldif-wrap=no", "-b", shellQuote(dn), "-s", "base", shellQuote("(objectClass=*)"), shellQuote(attr))
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, nil
	}
	entries := parseLdif(res.Stdout)
	if len(entries) == 0 {
		return nil, nil
	}
	return entries[0].valuesOf(attr), nil
}

// ldapModlistEntry renders one modlist hunk for Extra["modlist"] — see
// moduleLdapAttrs's own doc comment for why this port's modlist shape
// deliberately differs from real ldap_attrs's own.
func ldapModlistEntry(op, attr string, values [][]byte) map[string]any {
	strs := make([]string, len(values))
	for i, v := range values {
		strs[i] = string(v)
	}
	return map[string]any{"op": op, "attr": attr, "values": strs}
}
