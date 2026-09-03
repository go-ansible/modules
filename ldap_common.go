package modules

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what ldap_attrs.go, ldap_entry.go,
// ldap_passwd.go, and ldap_search.go share: parsing the connection
// arguments every real community.general.ldap_* module accepts via its
// `_ldap.documentation` doc fragment, rendering them into ldapsearch/
// ldapmodify/ldapadd/ldapdelete/ldappasswd/ldapwhoami CLI flags, LDIF
// parsing/building, and X-ORDERed DN discovery.
//
// Real ldap_* modules talk to the directory directly via python-ldap
// (libldap's own C bindings, via the collection's private
// module_utils/_ldap.py). This port has no Python/python-ldap on the
// target to lean on, so — matching the stance htpasswd.go and
// java_cert.go's own doc comments already take for their own external-
// tool dependencies — every ldap_* module here shells out to the real
// ldapsearch/ldapmodify/ldapadd/ldapdelete/ldappasswd/ldapwhoami
// command-line tools (from the ldap-utils/openldap-clients package)
// instead, and hard-requires whichever of them it needs to be present
// in PATH on the target, failing cleanly via Result{Failed:true} (not
// a Go error) if `command -v` comes up empty — see ldapRequireBinary.
//
// Connection-argument mapping (verified against the installed
// ldapsearch(1)/ldapmodify(1)/ldapdelete(1)/ldappasswd(1)/
// ldapwhoami(1) man pages and against module_utils/_ldap.py's actual
// LdapGeneric._connect_to_ldap, not guessed):
//
//   - server_uri -> `-H`.
//   - bind_dn: this port distinguishes "argument key absent from the
//     args map" from "argument present but set to the empty string",
//     exactly like real _connect_to_ldap's own `if self.bind_dn is not
//     None` check does (Ansible's own argspec machinery is what turns
//     an omitted YAML key into Python None here) — bind_dn omitted ->
//     SASL bind (`-Y EXTERNAL` or `-Y GSSAPI`, per sasl_class); bind_dn
//     present and "" -> anonymous simple bind (`-x` alone); bind_dn
//     present and non-empty -> simple bind (`-x -D <bind_dn>`, plus
//     `-y <tmpfile>` if bind_pw is non-empty).
//   - bind_pw is never placed on a command line, matching this
//     project's own "never put a secret in a command line" rule: it is
//     written to a target-side temp file via conn.TempPath+`cat >`
//     (stdin, `umask 077`), passed with the real tools' own `-y`
//     option, then removed — the same temp-file lifecycle java_cert.go
//     already uses for its own certificate content.
//   - start_tls=true -> `-Z`.
//   - validate_certs=false -> `LDAPTLS_REQCERT=never`, ca_path ->
//     `LDAPTLS_CACERT=<path>`, client_cert -> `LDAPTLS_CERT=<path>`,
//     client_key -> `LDAPTLS_KEY=<path>`, all as plain (non-secret)
//     environment assignments prefixed to the command line — these are
//     libldap's own documented TLS environment variables (ldap.conf(5)),
//     matching real _connect_to_ldap's OPT_X_TLS_REQUIRE_CERT/
//     OPT_X_TLS_CACERTFILE/OPT_X_TLS_CERTFILE/OPT_X_TLS_KEYFILE calls.
//   - referrals_chasing: NOT reproducible by this port and left as a
//     deliberate, documented gap rather than a silently wrong answer.
//     Real referrals_chasing=disabled calls python-ldap's own
//     connection.set_option(ldap.OPT_REFERRALS, 0) on the live
//     connection object; the CLI tools this port shells out to have no
//     per-invocation equivalent — ldap.conf(5)'s own REFERRALS
//     directive exists, but its man page states plainly "the command
//     line tools ldapsearch(1) & co always override this option". The
//     argument is still validated (disabled|anonymous) for
//     compatibility with real playbooks, but has no effect here.
//   - sasl_class (external|gssapi) is honored only for the SASL-bind
//     case above, exactly like real _ldap.py's own SASCL_CLASS lookup
//     is meaningless once a simple bind is in play.
//   - xorder_discovery: see ldapResolveDN.
//   - client_cert and client_key are required together (matching real
//     ldap_required_together()).
type ldapConn struct {
	serverURI        string
	bindDN           string
	bindDNSet        bool
	bindPW           string
	startTLS         bool
	validateCerts    bool
	caPath           string
	clientCert       string
	clientKey        string
	referralsChasing string
	saslClass        string
	xorderDiscovery  string
}

// parseLdapConn parses the connection arguments shared by every ldap_*
// module — see the ldapConn doc comment above for the exact mapping.
func parseLdapConn(args map[string]any) (ldapConn, error) {
	c := ldapConn{
		serverURI:        argString(args, "server_uri", "ldapi:///"),
		bindPW:           argString(args, "bind_pw", ""),
		startTLS:         argBool(args, "start_tls", false),
		validateCerts:    argBool(args, "validate_certs", true),
		caPath:           argString(args, "ca_path", ""),
		clientCert:       argString(args, "client_cert", ""),
		clientKey:        argString(args, "client_key", ""),
		referralsChasing: argString(args, "referrals_chasing", "anonymous"),
		saslClass:        argString(args, "sasl_class", "external"),
		xorderDiscovery:  argString(args, "xorder_discovery", "auto"),
	}
	if v, ok := args["bind_dn"]; ok {
		c.bindDNSet = true
		if s, ok2 := v.(string); ok2 {
			c.bindDN = s
		} else {
			c.bindDN = fmt.Sprint(v)
		}
	}
	if c.referralsChasing != "disabled" && c.referralsChasing != "anonymous" {
		return c, errArg("referrals_chasing must be disabled or anonymous, got %q", c.referralsChasing)
	}
	if c.saslClass != "external" && c.saslClass != "gssapi" {
		return c, errArg("sasl_class must be external or gssapi, got %q", c.saslClass)
	}
	if c.xorderDiscovery != "enable" && c.xorderDiscovery != "auto" && c.xorderDiscovery != "disable" {
		return c, errArg("xorder_discovery must be enable, auto, or disable, got %q", c.xorderDiscovery)
	}
	if (c.clientCert == "") != (c.clientKey == "") {
		return c, errArg("client_cert and client_key must be given together")
	}
	return c, nil
}

// envPrefix renders the TLS-related connection args as a leading
// "VAR=val VAR2=val2 " shell environment-assignment prefix. These are
// all non-secret (a boolean and file paths), so prefixing them inline
// on the command line does not run afoul of this project's "never put
// a secret in a command line" rule.
func (c ldapConn) envPrefix() string {
	var parts []string
	if !c.validateCerts {
		parts = append(parts, "LDAPTLS_REQCERT=never")
	}
	if c.caPath != "" {
		parts = append(parts, "LDAPTLS_CACERT="+shellQuote(c.caPath))
	}
	if c.clientCert != "" {
		parts = append(parts, "LDAPTLS_CERT="+shellQuote(c.clientCert))
	}
	if c.clientKey != "" {
		parts = append(parts, "LDAPTLS_KEY="+shellQuote(c.clientKey))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

// cmd renders one full ldap-utils invocation: the TLS env prefix, the
// binary name, the connection/auth flags, and any op-specific
// arguments (already shell-quoted by the caller).
func (c ldapConn) cmd(binary string, flags []string, rest ...string) string {
	parts := make([]string, 0, len(flags)+len(rest)+1)
	parts = append(parts, binary)
	parts = append(parts, flags...)
	parts = append(parts, rest...)
	return c.envPrefix() + strings.Join(parts, " ")
}

// ldapAuthFlags returns the -H/-Z/auth flags shared by every ldap-utils
// invocation this port makes on the ADMIN identity (bind_dn/bind_pw/
// sasl_class), plus a cleanup func that MUST be called (even on error)
// to remove any temp bind-password file it wrote.
func ldapAuthFlags(ctx context.Context, conn remoteexec.Connection, c ldapConn) (flags []string, cleanup func(), err error) {
	cleanup = func() {}
	flags = append(flags, "-H", shellQuote(c.serverURI))
	if c.startTLS {
		flags = append(flags, "-Z")
	}
	switch {
	case !c.bindDNSet:
		mech := "EXTERNAL"
		if c.saslClass == "gssapi" {
			mech = "GSSAPI"
		}
		flags = append(flags, "-Y", mech)
	case c.bindDN == "":
		flags = append(flags, "-x")
	default:
		flags = append(flags, "-x", "-D", shellQuote(c.bindDN))
		if c.bindPW != "" {
			path, cln, werr := ldapWriteTempFile(ctx, conn, "ldap-bindpw", c.bindPW)
			if werr != nil {
				return nil, cleanup, werr
			}
			cleanup = cln
			flags = append(flags, "-y", shellQuote(path))
		}
	}
	return flags, cleanup, nil
}

// ldapWriteTempFile writes content to a target-side temp file (named
// via conn.TempPath(base), restricted with `umask 077` before the
// write) and returns a cleanup func that removes it — the same
// pattern java_cert.go already uses for its own temp certificate
// files. Used for anything this port must not put on a command line.
func ldapWriteTempFile(ctx context.Context, conn remoteexec.Connection, base, content string) (path string, cleanup func(), err error) {
	path = conn.TempPath(base)
	cleanup = func() { _ = conn.Remove(ctx, path) }
	if _, err = conn.Exec(ctx, "umask 077 && cat > "+shellQuote(path), strings.NewReader(content)); err != nil {
		return "", func() {}, fmt.Errorf("writing %s: %w", base, err)
	}
	return path, cleanup, nil
}

// ldapRequireBinary fails cleanly (Result{Failed:true}, not a Go
// error) if bin is not on the target's PATH, naming which real
// ldap-utils tool moduleName needs and why (see this file's own
// package-level-style doc comment above for the full rationale).
func ldapRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName, bin string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v "+bin); err != nil {
		return Fail(fmt.Sprintf("%s: the %s binary is required on the target and was not found in PATH "+
			"(this port shells out to the real ldap-utils tools rather than linking python-ldap — "+
			"see ldap_common.go's own doc comment)", moduleName, bin)), false
	}
	return Result{}, true
}

// ldapXOrderPattern matches an X-ORDERed index prefix like "{0}"
// anywhere in an RDN or attribute value, mirroring real _xorder_dn's
// own `r".+\{\d+\}.+"` regex (applied to the DN's first RDN component)
// and _order_values' own `r"^\{\d+\}"` (applied to a value's own
// leading prefix, when re-numbering).
var (
	ldapXOrderDNPattern    = regexp.MustCompile(`.+\{[0-9]+\}.+`)
	ldapXOrderValuePattern = regexp.MustCompile(`^\{[0-9]+\}`)
)

// ldapOrderValues prepends fresh "{i}" X-ORDERed index numbers to
// values, replacing any existing "{n}" prefix each value already has —
// implementing the `ordered` argument shared by ldap_attrs and
// ldap_entry, matching real _order_values exactly.
func ldapOrderValues(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprintf("{%d}%s", i, ldapXOrderValuePattern.ReplaceAllString(v, ""))
	}
	return out
}

// splitDN splits dn into its RDN components on unescaped commas,
// trimming surrounding whitespace from each — enough to implement
// ldapResolveDN's own DN-parent/first-RDN split without a full RFC
// 4514 DN parser.
func splitDN(dn string) []string {
	var parts []string
	var cur strings.Builder
	esc := false
	for _, r := range dn {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\':
			cur.WriteRune(r)
			esc = true
		case r == ',':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}
	return parts
}

// ldapResolveDN implements the `xorder_discovery` argument: real
// ldap_* modules use python-ldap's ldap.dn.explode_dn to split dn into
// its RDN components, and when xorder_discovery is "enable" (or "auto"
// and the DN's first RDN does not already contain an X-ORDERed index
// like "{0}") issue an extra ONELEVEL search under the DN's parent,
// filtered on the first RDN, and — if it returns exactly one result —
// use ITS returned dn instead of the one passed in (the real, indexed
// DN; this is mainly useful for managing olcAccess-style X-ORDERed
// cn=config entries by an unindexed name). xorder_discovery=disable
// always uses dn exactly as given, which is also what this port falls
// back to if the extra search comes up empty, ambiguous, or errors —
// matching real _find_dn()'s own "except Exception: pass" swallow-and-
// keep-original behavior. flags must already include the caller's
// auth flags (ldapAuthFlags's own return value).
func ldapResolveDN(ctx context.Context, conn remoteexec.Connection, c ldapConn, flags []string, dn string) string {
	if c.xorderDiscovery == "disable" {
		return dn
	}
	rdns := splitDN(dn)
	if len(rdns) < 2 {
		return dn
	}
	first := rdns[0]
	if c.xorderDiscovery == "auto" && ldapXOrderDNPattern.MatchString(first) {
		return dn
	}
	parent := strings.Join(rdns[1:], ",")
	filter := "(" + first + ")"
	cmd := c.cmd("ldapsearch", flags, "-LLL", "-o", "ldif-wrap=no", "-b", shellQuote(parent), "-s", "one", shellQuote(filter), "dn")
	res, err := runStatus(ctx, conn, cmd)
	if err != nil || res.RC != 0 {
		return dn
	}
	entries := parseLdif(res.Stdout)
	if len(entries) == 1 && entries[0].dn != "" {
		return entries[0].dn
	}
	return dn
}

// ldapLdifEntry is one parsed LDIF entry: dn plus attribute name ->
// values (in the order ldapsearch printed them).
type ldapLdifEntry struct {
	dn    string
	attrs map[string][]string
}

// valuesOf returns entry's values for attr, matched case-insensitively
// — LDAP attribute names are case-insensitive, and the server may echo
// back a different case than what was requested (real _get_all_values_of
// does the same `k.lower() == lc_name` case-insensitive lookup).
func (e ldapLdifEntry) valuesOf(attr string) []string {
	lc := strings.ToLower(attr)
	for k, v := range e.attrs {
		if strings.ToLower(k) == lc {
			return v
		}
	}
	return nil
}

// parseLdif parses ldapsearch's own LDIF output (this port always runs
// ldapsearch with "-LLL -o ldif-wrap=no", so it never has to strip
// "# comment"/"version:" header lines or un-wrap folded continuation
// lines) into a list of entries, one per blank-line-separated block.
// A value marked with "::" (base64, RFC 2849) is decoded into its raw
// bytes; a value that fails to decode is skipped rather than erroring
// the whole parse.
func parseLdif(out string) []ldapLdifEntry {
	var entries []ldapLdifEntry
	var cur *ldapLdifEntry
	flush := func() {
		if cur != nil && cur.dn != "" {
			entries = append(entries, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if cur == nil {
			cur = &ldapLdifEntry{attrs: map[string][]string{}}
		}
		attr, val, ok := parseLdifLine(line)
		if !ok {
			continue
		}
		if attr == "dn" {
			cur.dn = val
			continue
		}
		cur.attrs[attr] = append(cur.attrs[attr], val)
	}
	flush()
	return entries
}

// parseLdifLine splits one "attr: value", "attr:: base64value", or
// bare "attr:" (attributesonly mode, `-A`) LDIF line.
func parseLdifLine(line string) (attr, val string, ok bool) {
	if i := strings.Index(line, ":: "); i >= 0 {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line[i+3:]))
		if err != nil {
			return "", "", false
		}
		return line[:i], string(decoded), true
	}
	if i := strings.Index(line, ": "); i >= 0 {
		return line[:i], line[i+2:], true
	}
	if strings.HasSuffix(line, ":") {
		return line[:len(line)-1], "", true
	}
	return "", "", false
}

// ldifSafeValue reports whether value can be written as a plain
// "attr: value" LDIF line, per RFC 2849's SAFE-STRING rule (7-bit-clean
// US-ASCII, not starting with a space/colon/less-than, and containing
// no NUL/LF/CR) — conservative but simple and always correct; anything
// else is base64-encoded instead ("attr:: ...").
func ldifSafeValue(v []byte) bool {
	if len(v) == 0 {
		return true
	}
	switch v[0] {
	case ' ', ':', '<':
		return false
	}
	for _, b := range v {
		if b == 0 || b == '\n' || b == '\r' || b >= 0x80 {
			return false
		}
	}
	return true
}

// ldifAttrLine renders one LDIF attribute line for attr/value, using
// base64 ("::") when the value is not RFC 2849 SAFE-STRING-safe (see
// ldifSafeValue) — used for every LDIF this port builds and sends to
// ldapadd/ldapmodify (including the "dn:" line itself).
func ldifAttrLine(attr string, value []byte) string {
	if ldifSafeValue(value) {
		return attr + ": " + string(value)
	}
	return attr + ":: " + base64.StdEncoding.EncodeToString(value)
}

// ldapValueStrings normalizes an "attributes" dict value (a single
// scalar or a list, as YAML/JSON hands it to this port) into a string
// slice.
func ldapValueStrings(v any) []string {
	switch val := v.(type) {
	case []any:
		out := make([]string, len(val))
		for i, x := range val {
			out[i] = fmt.Sprint(x)
		}
		return out
	case []string:
		return val
	case nil:
		return nil
	default:
		return []string{fmt.Sprint(val)}
	}
}

// ldapIsBinaryAttr reports whether attr must be treated as a raw byte
// sequence (Base64-decoded before sending) rather than a text string —
// matching real _is_binary exactly: true if honorBinary is set and
// attr has a ";binary" option (RFC 4522), or if attr's name (case-
// insensitive) is listed in binarySet.
func ldapIsBinaryAttr(attr string, honorBinary bool, binarySet map[string]bool) bool {
	lc := strings.ToLower(attr)
	if binarySet[lc] {
		return true
	}
	if honorBinary {
		for _, opt := range strings.Split(lc, ";") {
			if opt == "binary" {
				return true
			}
		}
	}
	return false
}

// ldapNormalizeAttrValues turns one "attributes" dict entry's raw value
// into the byte-slice values actually sent to the directory: ordered
// re-numbering first (text attributes only, matching real
// _normalize_values' own "elif self.ordered and not is_binary" order of
// operations), then either UTF-8 bytes or a Base64 decode per isBinary.
func ldapNormalizeAttrValues(rawValue any, isBinary, ordered bool) ([][]byte, error) {
	strs := ldapValueStrings(rawValue)
	if ordered && !isBinary {
		strs = ldapOrderValues(strs)
	}
	out := make([][]byte, len(strs))
	for i, s := range strs {
		if isBinary {
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return nil, err
			}
			out[i] = b
		} else {
			out[i] = []byte(s)
		}
	}
	return out, nil
}

// sortedKeys returns m's keys, sorted — used everywhere this port
// iterates an "attributes" dict, so the LDIF/command it builds is
// deterministic (and thus testable).
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// utf8Replace returns s with any invalid UTF-8 byte sequence replaced
// by U+FFFD, matching Python's `to_text(val, "utf-8", errors="replace")`
// — used by ldap_search.go when rendering an attribute value that was
// NOT requested via base64_attributes.
func utf8Replace(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
			i++
		} else {
			b.WriteRune(r)
			i += size
		}
	}
	return b.String()
}
