package modules

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// udmBin is the command-line client this port shells out to for every
// udm_*.go module in this file's family (udm_dns_record, udm_dns_zone,
// udm_group, udm_share, udm_user). Real community.general's udm_*
// modules are all implemented against Univention Corporate Server
// (UCS)'s own Python API — univention.admin's UMC object model, reached
// via a local uldap/UMC connection through the
// ansible_collections.community.general.plugins.module_utils._univention_umc
// helper — and are documented as requiring the "Univention" Python
// bindings, i.e. running ON a UCS server itself. This port has no Go
// binding for that Python API, so it substitutes UCS's own `udm`
// command-line tool instead: the same tool a UCS administrator normally
// runs by hand (`udm <module> create|modify|remove|list`) — matching
// the substitution this project already makes for lxd_container.go
// (pylxd -> `lxc`) and ldap_entry.go (python-ldap ->
// ldapsearch/ldapadd/ldapdelete). See each module's own doc comment for
// the specific fidelity gaps this implies.
const udmBin = "udm"

// udmObject is one object as reported by `udm <module> list`: its DN
// (from the "DN: <dn>" header line) plus every "  <attr>: <value>" line
// that follows, grouped by attribute name in the order the CLI printed
// them — a multi-valued attribute prints as one line per value, and an
// attribute with no value at all prints as "  <attr>: " with nothing
// after the colon-space (parsed here as one empty-string value), both
// matching the real `udm list` text formatter.
type udmObject struct {
	DN    string
	Attrs map[string][]string
}

// udmRun quotes and runs one `udm` invocation.
func udmRun(ctx context.Context, conn remoteexec.Connection, argv []string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, strings.Join(quoted, " "), nil)
}

// udmBaseDN resolves the UCS LDAP base DN via `ucr get ldap/base` — the
// same Univention Configuration Registry lookup a shell script uses to
// learn $(ucr get ldap/base), and the value real udm_*'s own Python API
// exposes as base_dn().
func udmBaseDN(ctx context.Context, conn remoteexec.Connection) (string, error) {
	out, err := run(ctx, conn, "ucr get ldap/base")
	if err != nil {
		return "", fmt.Errorf("udm: resolving ldap/base via ucr: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("udm: ucr get ldap/base returned an empty value")
	}
	return out, nil
}

// udmParseList parses `udm <module> list`'s own plain-text output into
// one udmObject per "DN: <dn>" header line and the indented attribute
// lines that follow it, up to the next "DN: " line or end of output.
func udmParseList(out string) []udmObject {
	var objs []udmObject
	var cur *udmObject
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "DN: ") {
			if cur != nil {
				objs = append(objs, *cur)
			}
			cur = &udmObject{DN: strings.TrimSpace(strings.TrimPrefix(line, "DN: ")), Attrs: map[string][]string{}}
			continue
		}
		if cur == nil || !strings.HasPrefix(line, "  ") {
			continue
		}
		body := strings.TrimPrefix(line, "  ")
		if idx := strings.Index(body, ": "); idx >= 0 {
			key := body[:idx]
			cur.Attrs[key] = append(cur.Attrs[key], body[idx+2:])
		} else if strings.HasSuffix(body, ":") {
			key := strings.TrimSuffix(body, ":")
			cur.Attrs[key] = append(cur.Attrs[key], "")
		}
	}
	if cur != nil {
		objs = append(objs, *cur)
	}
	return objs
}

// udmScope selects, for udmFind/udmCreate, whether a udm object type is
// created directly under a container (Position, passed as `--position`
// — e.g. groups/group, shares/share, users/user, dns/forward_zone,
// dns/reverse_zone) or as a subordinate of another already-existing
// object (Superordinate, passed as `--superordinate` — e.g.
// dns/host_record and the other dns/*_record types, which live inside a
// dns/forward_zone or dns/reverse_zone). This mirrors real
// udm_dns_record.py's own `umc_module_for_add(..., superordinate=so[0])`
// call, the one place among this module family that isn't a plain
// top-level `--position` create.
type udmScope struct {
	Position      string // used when non-empty
	Superordinate string // used when Position is empty
}

func (s udmScope) findArgs() []string {
	if s.Superordinate != "" {
		return []string{"--superordinate", s.Superordinate}
	}
	return nil
}

func (s udmScope) createArgs() []string {
	if s.Position != "" {
		return []string{"--position", s.Position}
	}
	return []string{"--superordinate", s.Superordinate}
}

// udmFind looks up the first object matching filter within scope,
// returning (nil, nil) if none is found. Like every real udm_* module
// in this family (each of which searches only by its own object's
// primary identifying property, e.g. cn or uid, never scoped to a
// specific container), this does not disambiguate two objects with the
// same identifying property value living in different containers —
// matching real udm_*.py's own identical narrowing, not a gap this port
// introduces on its own.
func udmFind(ctx context.Context, conn remoteexec.Connection, modulePath, filter string, scope udmScope) (*udmObject, error) {
	argv := append([]string{udmBin, modulePath, "list", "--filter", filter}, scope.findArgs()...)
	res, err := udmRun(ctx, conn, argv)
	if err != nil {
		return nil, fmt.Errorf("udm: listing %s: %w", modulePath, err)
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("udm: %s list --filter %s: %s", modulePath, filter, strings.TrimSpace(res.Stderr))
	}
	objs := udmParseList(res.Stdout)
	if len(objs) == 0 {
		return nil, nil
	}
	return &objs[0], nil
}

func udmSortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func udmSetArgs(sets map[string][]string) []string {
	var argv []string
	for _, k := range udmSortedKeys(sets) {
		for _, v := range sets[k] {
			argv = append(argv, "--set", k+"="+v)
		}
	}
	return argv
}

// udmCreate runs `udm <modulePath> create` with scope's own
// --position/--superordinate flag plus one `--set key=value` per
// desired attribute value (multi-valued attributes get one --set per
// value, in sorted-key order for deterministic command construction —
// matching lxd_container.go's own lxdReconcileConfig convention).
func udmCreate(ctx context.Context, conn remoteexec.Connection, modulePath string, scope udmScope, sets map[string][]string) error {
	argv := append([]string{udmBin, modulePath, "create"}, scope.createArgs()...)
	argv = append(argv, udmSetArgs(sets)...)
	res, err := udmRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("udm: creating %s: %s", modulePath, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// udmReconcile diffs desired against obj.Attrs (as unordered sets of
// values per attribute — this port does not attempt to preserve a
// multi-valued attribute's own value ORDER, only its membership) and,
// if anything differs, issues a single `udm <modulePath> modify --dn
// <dn>` carrying one `--remove key=value` per value present now but not
// desired and one `--set key=value` per value desired but not present
// now (which the real `udm` CLI treats as "add value" for an
// already-multi-valued property, and as "replace" for a single-valued
// one). This is cheaper and more explicit than real udm_*'s own Python
// API `.diff()`/`.hasChanged()`, but observably equivalent for every
// attribute this port actually manages. Only attributes present in
// desired are considered — an attribute the caller didn't mention is
// left untouched, matching every real udm_* module here (none of which
// clears an attribute the caller didn't set).
func udmReconcile(ctx context.Context, conn remoteexec.Connection, modulePath string, obj *udmObject, desired map[string][]string) (bool, error) {
	var argv []string
	for _, k := range udmSortedKeys(desired) {
		want := desired[k]
		cur := obj.Attrs[k]
		if stringSetEqual(cur, want) {
			continue
		}
		for _, v := range cur {
			if !containsString(want, v) {
				argv = append(argv, "--remove", k+"="+v)
			}
		}
		for _, v := range want {
			if !containsString(cur, v) {
				argv = append(argv, "--set", k+"="+v)
			}
		}
	}
	if len(argv) == 0 {
		return false, nil
	}
	full := append([]string{udmBin, modulePath, "modify", "--dn", obj.DN}, argv...)
	res, err := udmRun(ctx, conn, full)
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("udm: modifying %s: %s", obj.DN, strings.TrimSpace(res.Stderr))
	}
	return true, nil
}

// udmAppend runs `udm <modulePath> modify --dn <dn> --append key=value`
// unconditionally — used by udm_user.go to add a user to a group's own
// multi-valued "users" attribute without first re-reading the group's
// full current member list (the caller is expected to have already
// checked membership via a udmFind on the group).
func udmAppend(ctx context.Context, conn remoteexec.Connection, modulePath, dn, key, value string) error {
	argv := []string{udmBin, modulePath, "modify", "--dn", dn, "--append", key + "=" + value}
	res, err := udmRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("udm: appending %s=%s to %s: %s", key, value, dn, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func udmRemove(ctx context.Context, conn remoteexec.Connection, modulePath, dn string) error {
	argv := []string{udmBin, modulePath, "remove", "--dn", dn}
	res, err := udmRun(ctx, conn, argv)
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("udm: removing %s: %s", dn, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// udmBoolStr renders a bool the way real udm_share.py's own
// module.params -> "1"/"0" conversion does for boolean UDM properties.
func udmBoolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// argStringAliased reads args[primary], falling back to args[alias]
// then def — for the many udm_share/udm_user options whose real
// argument_spec key is a camelCase UDM attribute name (e.g.
// "sambaBrowseable") with a snake_case Ansible-style alias (e.g.
// "samba_browsable"); either may be the key actually present in a given
// task's args map.
func argStringAliased(args map[string]any, primary, alias, def string) string {
	if _, ok := args[primary]; ok {
		return argString(args, primary, def)
	}
	if alias != "" {
		if _, ok := args[alias]; ok {
			return argString(args, alias, def)
		}
	}
	return def
}

func argBoolAliased(args map[string]any, primary, alias string, def bool) bool {
	if _, ok := args[primary]; ok {
		return argBool(args, primary, def)
	}
	if alias != "" {
		if _, ok := args[alias]; ok {
			return argBool(args, alias, def)
		}
	}
	return def
}

func argStringListAliased(args map[string]any, primary, alias string) []string {
	if v := argStringList(args, primary); v != nil {
		return v
	}
	if alias != "" {
		return argStringList(args, alias)
	}
	return nil
}

// udmReversePointer renders ip the same way Python's own
// ipaddress.ip_address(ip).reverse_pointer does — the in-addr.arpa (v4)
// or ip6.arpa (v6) name real udm_dns_record.py computes for a PTR
// record's own relativeDomainName — since this port has no such stdlib
// helper of its own. Returns an error for an unparseable address.
func udmReversePointer(ip string) (string, error) {
	addr := net.ParseIP(ip)
	if addr == nil {
		return "", fmt.Errorf("udm: %q is not a valid IP address", ip)
	}
	if v4 := addr.To4(); v4 != nil && strings.Contains(ip, ".") {
		return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", v4[3], v4[2], v4[1], v4[0]), nil
	}
	v6 := addr.To16()
	hexStr := hex.EncodeToString(v6)
	var b strings.Builder
	for i := len(hexStr) - 1; i >= 0; i-- {
		b.WriteByte(hexStr[i])
		b.WriteByte('.')
	}
	b.WriteString("ip6.arpa")
	return b.String(), nil
}
