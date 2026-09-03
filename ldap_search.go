package modules

import (
	"context"
	"encoding/base64"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLdapSearch implements (a subset of) Ansible's `ldap_search`
// module: a read-only LDAP search, returning matching entries. See
// ldap_common.go's own doc comment for this port's shared "shells out
// to the real ldapsearch binary instead of linking python-ldap"
// architecture and its connection-argument mapping (server_uri,
// bind_dn, bind_pw, start_tls, validate_certs, ca_path, client_cert,
// client_key, referrals_chasing, sasl_class, xorder_discovery) — all
// accepted here too, with the same semantics and the same documented
// gaps (referrals_chasing has no CLI equivalent).
//
// Args: dn (string, required) — the search base; scope (base|onelevel|
// subordinate|children, default "base") — mapped to ldapsearch's own
// `-s {base|one|sub|children}` flag values as base->base, onelevel->one,
// subordinate->children (children is ldapsearch's own name for the
// RFC 3673 subordinate-subtree scope, per both `man ldapsearch` and
// real module_utils/_ldap.py's own SCOPE_SUBORDINATE mapping),
// children->sub (real "children" choice is documented as "equivalent
// to a subtree scope", i.e. ldap.SCOPE_SUBTREE, i.e. ldapsearch's own
// "sub" — this mapping was verified against the installed ldapsearch(1)
// man page and the real module's own _load_scope, not guessed); filter
// (string, default "(objectClass=*)"); attrs (list of string, optional
// — when empty, every attr's values are returned, matching ldapsearch's
// own "no attrs listed" default); schema (bool, default false) — maps
// to ldapsearch's own `-A` ("retrieve attribute names only, no
// values") flag, matching real schema's own attrsonly=1; page_size
// (int, default 0) — 0 disables paged search (default), any other
// value uses ldapsearch's own `-E pr=<n>/noprompt` simple-paged-results
// control; base64_attributes (list of string, optional; "*" means
// "every attribute") — controls how each returned VALUE is rendered:
// an attribute value is decoded from LDIF back into raw bytes
// regardless of whether ldapsearch itself printed it plain or
// Base64-encoded (RFC 2849 "::"), then re-encoded to Base64 if its
// attribute name is listed here (or "*" is), otherwise coerced to a
// UTF-8 string with any invalid byte sequence replaced by U+FFFD
// (matching real _normalize_string's own to_text(..., errors="replace")
// — note real module's own doc text says such bytes are "omitted",
// but its actual code replaces them; this port matches the real code,
// not the real doc text, on this specific point).
//
// Unlike this port's other modules, ldap_search's real return value is
// NOT nested under ansible_facts (real ldap_search does
// `module.exit_json(changed=False, results=results)`, a plain
// top-level return, not `ansible_facts.something`) — matching that
// exactly, results are returned here as Extra["results"], a list of
// maps. In schema mode each map is {"dn": ..., "attrs": [name, ...]}
// (sorted); otherwise each map is {"dn": ..., <attr>: value} where
// value is a single string if the attribute has exactly one value, or
// a list of strings otherwise — exactly matching real _extract_entry.
//
// Simplifications vs real ldap_search: xorder_discovery is applied to
// dn (the search base) exactly like every other ldap_* module here,
// but real ldap_search's own perform_search fails the whole search
// with "Base not found: <dn>" specifically on a missing base object;
// this port instead returns a generic Fail with ldapsearch's own
// stderr, since distinguishing "no such object" from ldapsearch's
// other failure modes by parsing its stderr text was judged not worth
// the fragility.
func moduleLdapSearch(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dn, err := requireString(args, "dn")
	if err != nil {
		return Result{}, err
	}
	scope := argString(args, "scope", "base")
	scopeFlag, err := ldapSearchScopeFlag(scope)
	if err != nil {
		return Result{}, err
	}
	filter := argString(args, "filter", "(objectClass=*)")
	attrs := argStringList(args, "attrs")
	schema := argBool(args, "schema", false)
	pageSize := argInt(args, "page_size", 0)

	base64List := argStringList(args, "base64_attributes")
	allBase64 := false
	base64Set := map[string]bool{}
	for _, a := range base64List {
		if a == "*" {
			allBase64 = true
		}
		base64Set[strings.ToLower(a)] = true
	}

	c, err := parseLdapConn(args)
	if err != nil {
		return Result{}, err
	}
	if res, ok := ldapRequireBinary(ctx, conn, "ldap_search", "ldapsearch"); !ok {
		return res, nil
	}

	flags, cleanup, err := ldapAuthFlags(ctx, conn, c)
	defer cleanup()
	if err != nil {
		return Result{}, err
	}

	resolvedDN := ldapResolveDN(ctx, conn, c, flags, dn)

	rest := []string{"-LLL", "-o", "ldif-wrap=no"}
	if schema {
		rest = append(rest, "-A")
	}
	if pageSize > 0 {
		rest = append(rest, "-E", shellQuote("pr="+strconv.Itoa(pageSize)+"/noprompt"))
	}
	rest = append(rest, "-b", shellQuote(resolvedDN), "-s", scopeFlag, shellQuote(filter))
	for _, a := range attrs {
		rest = append(rest, shellQuote(a))
	}

	cmd := c.cmd("ldapsearch", flags, rest...)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("ldap_search: search failed: " + strings.TrimSpace(res.Stderr)), nil
	}

	entries := parseLdif(res.Stdout)
	results := make([]any, 0, len(entries))
	for _, e := range entries {
		if schema {
			names := make([]string, 0, len(e.attrs))
			for name := range e.attrs {
				names = append(names, name)
			}
			sort.Strings(names)
			results = append(results, map[string]any{"dn": e.dn, "attrs": names})
			continue
		}
		entry := map[string]any{"dn": e.dn}
		for name, vals := range e.attrs {
			entry[name] = ldapSearchAttrValue(vals, name, base64Set, allBase64)
		}
		results = append(results, entry)
	}

	return Ok("").WithExtra("results", results), nil
}

// ldapSearchScopeFlag maps the "scope" argument to ldapsearch's own
// `-s` flag value — see moduleLdapSearch's doc comment for how this
// mapping was verified.
func ldapSearchScopeFlag(scope string) (string, error) {
	switch scope {
	case "base":
		return "base", nil
	case "onelevel":
		return "one", nil
	case "subordinate":
		return "children", nil
	case "children":
		return "sub", nil
	default:
		return "", errArg("ldap_search: scope must be base, onelevel, subordinate, or children, got %q", scope)
	}
}

// ldapSearchAttrValue renders one attribute's parsed LDIF values (raw
// bytes, already decoded from "::" Base64 if that's how ldapsearch
// printed them) as either a single string or a list, per
// base64_attributes — see moduleLdapSearch's own doc comment.
func ldapSearchAttrValue(vals []string, name string, base64Set map[string]bool, allBase64 bool) any {
	toBase64 := allBase64 || base64Set[strings.ToLower(name)]
	out := make([]string, len(vals))
	for i, v := range vals {
		if toBase64 {
			out[i] = base64.StdEncoding.EncodeToString([]byte(v))
		} else {
			out[i] = utf8Replace(v)
		}
	}
	if len(out) == 1 {
		return out[0]
	}
	return out
}
