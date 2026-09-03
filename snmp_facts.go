package modules

import (
	"context"
	"encoding/hex"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSNMPFacts implements (a subset of) Ansible's `snmp_facts`
// (community.general) module: gathers read-only device facts over SNMP
// — read from real snmp_facts.py's own OID table and varBinds-walking
// loops (this batch's hard rule: the exact scalar/table OIDs and the
// admin/oper-status and MAC/hex decoding rules below are only visible
// there, not EXAMPLES/RETURN VALUES).
//
// Real snmp_facts talks SNMP directly from the CONTROLLER, over the
// network, using the `pysnmp` Python library (its own REQUIREMENTS
// note names it explicitly) — it never runs anything on the target at
// all; `host` is a network address it dials into, unrelated to
// `conn`/the target this module actually executes against. This port
// has no Go SNMP library and, per this package's architecture, can
// only reach a target through Connection.Exec — so instead, per this
// batch's task description, it shells out to `snmpget`/`snmpwalk` (the
// net-snmp package's own CLI tools) ON THE TARGET, which then talk SNMP
// to `host` on the target's behalf. This is a genuine architectural
// substitution, not a syntax difference: it requires net-snmp installed
// on the target (a hard, target-side dependency this port did not
// invent — real snmp_facts' own dependency is pysnmp on the
// CONTROLLER instead), and its exact wire-level behavior (retry/
// timeout semantics, SNMPv3 engine discovery, etc.) is net-snmp's own,
// not pysnmp's — the OID set and resulting fact SHAPE are matched
// exactly, but the two are different SNMP client implementations
// talking to the same protocol.
//
// Args: host (string, required); version (v2|v2c|v3, required) — v2
// and v2c are both mapped to net-snmp's own `-v 2c` (real snmp_facts
// treats them identically too, both meaning community-based SNMPv2c);
// community (string, required for v2/v2c); for v3: username (required),
// level (authNoPriv|authPriv, required), integrity (md5|sha, required)
// -> net-snmp's own `-a MD5|SHA`, authkey (required) -> `-A`; privacy
// (aes|des, required if level=authPriv) -> `-x AES|DES`, privkey
// (required if level=authPriv) -> `-X`; timeout (int, seconds) -> `-t`;
// retries (int) -> `-r`.
//
// Scalar facts (ansible_sysdescr, ansible_sysobjectid, ansible_sysuptime,
// ansible_syscontact, ansible_sysname, ansible_syslocation) come from
// ONE `snmpget` of the six standard MIB-2 system.* OIDs
// (1.3.6.1.2.1.1.{1,2,3,4,5,6}.0); ansible_sysdescr additionally hex-
// decodes a "0x..."-prefixed value into text, matching real
// snmp_facts' own decode_hex. All scalar values (including
// ansible_sysuptime) are kept as STRINGS, matching real snmp_facts' own
// `val.prettyPrint()` — despite this module's own RETURN VALUES
// documenting ansible_sysuptime as `type: int`, the actual Python
// assignment (`results["ansible_sysuptime"] = current_val`, where
// current_val is always a str) never converts it, so this port matches
// the REAL (string) behavior over the (seemingly inaccurate) documented
// type, per this batch's hard rule to read the implementation rather
// than trust EXAMPLES/RETURN VALUES alone.
//
// ansible_interfaces is built from ONE `snmpwalk` per interface-table
// column (ifIndex/ifDescr/ifMtu/ifSpeed/ifPhysAddress/ifAdminStatus/
// ifOperStatus/ifAlias, each under 1.3.6.1.2.1.2.2.1.* or
// 1.3.6.1.2.1.31.1.1.1.18 for ifAlias), keyed by the trailing dotted
// index of each returned OID; ifPhysAddress is hex-decoded into a
// bare, colon-free hex string (matching real snmp_facts' own
// decode_mac — e.g. "000a305a52a1", not "00:0a:30:5a:52:a1"); admin/
// operstatus integers are mapped through real snmp_facts' own
// lookup_adminstatus ({1:up, 2:down, 3:testing}) and
// lookup_operstatus ({1:up, 2:down, 3:testing, 4:unknown, 5:dormant,
// 6:notPresent, 7:lowerLayerDown}) tables, defaulting to "" for an
// unrecognized code exactly as those two real functions do.
// ansible_all_ipv4_addresses and each interface's own "ipv4" list come
// from walking ipAdEntAddr/ipAdEntIfIndex/ipAdEntNetMask
// (1.3.6.1.2.1.4.20.1.{1,2,3}) — this table's own index IS the IP
// address's four octets, matching real snmp_facts' own `current_oid.
// rsplit(".", 4)[-4:]` extraction, reproduced identically here.
//
// This port issues each interface-table column as its own dedicated
// `snmpwalk` (rather than real snmp_facts' own single combined
// getBulk across all eleven table OIDs in one SNMP exchange) — a
// documented efficiency difference (more round trips), not a
// behavioral one: the resulting fact shape is identical either way,
// since each column is independently indexed by ifIndex/IP address
// regardless of how many SNMP requests it took to collect.
func moduleSNMPFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	host, err := requireString(args, "host")
	if err != nil {
		return Result{}, err
	}
	version, err := requireString(args, "version")
	if err != nil {
		return Result{}, err
	}
	if version != "v2" && version != "v2c" && version != "v3" {
		return Result{}, errArg("snmp_facts: version must be v2, v2c, or v3, got %q", version)
	}

	authArgs, ferr := snmpAuthArgs(args, version)
	if ferr != "" {
		return Fail("snmp_facts: " + ferr), nil
	}

	if _, err := run(ctx, conn, "command -v snmpget && command -v snmpwalk"); err != nil {
		return Fail("snmp_facts: snmpget/snmpwalk (net-snmp) not found on the target"), nil
	}

	common := authArgs
	if _, ok := args["timeout"]; ok {
		common += " -t " + shellQuote(strconv.Itoa(argInt(args, "timeout", 0)))
	}
	if _, ok := args["retries"]; ok {
		common += " -r " + shellQuote(strconv.Itoa(argInt(args, "retries", 0)))
	}

	results := map[string]any{}

	scalarOIDs := []string{
		"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0", "1.3.6.1.2.1.1.3.0",
		"1.3.6.1.2.1.1.4.0", "1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.1.6.0",
	}
	scalarCmd := "snmpget -O qn" + common + " " + shellQuote(host) + " " + strings.Join(scalarOIDs, " ")
	scalarOut, err := run(ctx, conn, scalarCmd)
	if err != nil {
		return Fail("snmp_facts: " + err.Error()), nil
	}
	scalars := snmpParseOutput(scalarOut)
	if v, ok := scalars["1.3.6.1.2.1.1.1.0"]; ok {
		results["ansible_sysdescr"] = snmpDecodeHex(v)
	}
	if v, ok := scalars["1.3.6.1.2.1.1.2.0"]; ok {
		results["ansible_sysobjectid"] = v
	}
	if v, ok := scalars["1.3.6.1.2.1.1.3.0"]; ok {
		results["ansible_sysuptime"] = v
	}
	if v, ok := scalars["1.3.6.1.2.1.1.4.0"]; ok {
		results["ansible_syscontact"] = v
	}
	if v, ok := scalars["1.3.6.1.2.1.1.5.0"]; ok {
		results["ansible_sysname"] = v
	}
	if v, ok := scalars["1.3.6.1.2.1.1.6.0"]; ok {
		results["ansible_syslocation"] = v
	}

	interfaces := map[int]map[string]any{}
	ensureIface := func(idx int) map[string]any {
		m, ok := interfaces[idx]
		if !ok {
			m = map[string]any{}
			interfaces[idx] = m
		}
		return m
	}

	walk := func(oidBase string) (map[string]string, error) {
		out, err := run(ctx, conn, "snmpwalk -O qn"+common+" "+shellQuote(host)+" "+shellQuote(oidBase))
		if err != nil {
			return nil, err
		}
		return snmpParseOutput(out), nil
	}

	columns := []struct {
		oid   string
		apply func(idx int, val string)
	}{
		{"1.3.6.1.2.1.2.2.1.1", func(idx int, v string) { ensureIface(idx)["ifindex"] = v }},
		{"1.3.6.1.2.1.2.2.1.2", func(idx int, v string) { ensureIface(idx)["name"] = v }},
		{"1.3.6.1.2.1.2.2.1.4", func(idx int, v string) { ensureIface(idx)["mtu"] = v }},
		{"1.3.6.1.2.1.2.2.1.5", func(idx int, v string) { ensureIface(idx)["speed"] = v }},
		{"1.3.6.1.2.1.2.2.1.6", func(idx int, v string) { ensureIface(idx)["mac"] = snmpDecodeMAC(v) }},
		{"1.3.6.1.2.1.2.2.1.7", func(idx int, v string) { ensureIface(idx)["adminstatus"] = snmpLookupAdminStatus(v) }},
		{"1.3.6.1.2.1.2.2.1.8", func(idx int, v string) { ensureIface(idx)["operstatus"] = snmpLookupOperStatus(v) }},
		{"1.3.6.1.2.1.31.1.1.1.18", func(idx int, v string) { ensureIface(idx)["description"] = v }},
	}
	for _, col := range columns {
		rows, err := walk(col.oid)
		if err != nil {
			return Fail("snmp_facts: " + err.Error()), nil
		}
		for oid, val := range rows {
			idx, ok := snmpTrailingIndex(oid, col.oid)
			if !ok {
				continue
			}
			col.apply(idx, val)
		}
	}

	type ipRow struct{ address, netmask, iface string }
	ipRows := map[string]*ipRow{}
	getRow := func(ip string) *ipRow {
		r, ok := ipRows[ip]
		if !ok {
			r = &ipRow{}
			ipRows[ip] = r
		}
		return r
	}
	var allIPv4 []string
	addrRows, err := walk("1.3.6.1.2.1.4.20.1.1")
	if err != nil {
		return Fail("snmp_facts: " + err.Error()), nil
	}
	for oid, val := range addrRows {
		if ip, ok := snmpTrailingIP(oid, "1.3.6.1.2.1.4.20.1.1"); ok {
			getRow(ip).address = val
			allIPv4 = append(allIPv4, val)
		}
	}
	ifIndexRows, err := walk("1.3.6.1.2.1.4.20.1.2")
	if err != nil {
		return Fail("snmp_facts: " + err.Error()), nil
	}
	for oid, val := range ifIndexRows {
		if ip, ok := snmpTrailingIP(oid, "1.3.6.1.2.1.4.20.1.2"); ok {
			getRow(ip).iface = val
		}
	}
	netmaskRows, err := walk("1.3.6.1.2.1.4.20.1.3")
	if err != nil {
		return Fail("snmp_facts: " + err.Error()), nil
	}
	for oid, val := range netmaskRows {
		if ip, ok := snmpTrailingIP(oid, "1.3.6.1.2.1.4.20.1.3"); ok {
			getRow(ip).netmask = val
		}
	}
	for _, r := range ipRows {
		idx, err := strconv.Atoi(r.iface)
		if err != nil {
			continue
		}
		iface := ensureIface(idx)
		list, _ := iface["ipv4"].([]map[string]any)
		list = append(list, map[string]any{"address": r.address, "netmask": r.netmask})
		iface["ipv4"] = list
	}

	ifaceOut := map[string]any{}
	for idx, m := range interfaces {
		ifaceOut[strconv.Itoa(idx)] = m
	}
	results["ansible_interfaces"] = ifaceOut
	results["ansible_all_ipv4_addresses"] = allIPv4

	return Ok("").WithExtra("ansible_facts", results), nil
}

// snmpAuthArgs builds the net-snmp CLI's own authentication flags for
// version, per moduleSNMPFacts' own doc comment. ferr is non-empty for
// a missing required argument, mirroring real snmp_facts' own
// required_if validation.
func snmpAuthArgs(args map[string]any, version string) (cmdArgs string, ferr string) {
	if version == "v2" || version == "v2c" {
		community, err := requireString(args, "community")
		if err != nil {
			return "", "community is required for version v2/v2c"
		}
		return " -v 2c -c " + shellQuote(community), ""
	}

	username := argString(args, "username", "")
	level := argString(args, "level", "")
	integrity := argString(args, "integrity", "")
	authkey := argString(args, "authkey", "")
	if username == "" || level == "" || integrity == "" || authkey == "" {
		return "", "username, level, integrity, and authkey are required for version v3"
	}
	if level != "authNoPriv" && level != "authPriv" {
		return "", "level must be authNoPriv or authPriv"
	}
	integrityProto := map[string]string{"md5": "MD5", "sha": "SHA"}[integrity]
	if integrityProto == "" {
		return "", "integrity must be md5 or sha"
	}
	cmdArgs = " -v 3 -u " + shellQuote(username) + " -l " + shellQuote(level) +
		" -a " + integrityProto + " -A " + shellQuote(authkey)

	if level == "authPriv" {
		privacy := argString(args, "privacy", "")
		privkey := argString(args, "privkey", "")
		if privacy == "" || privkey == "" {
			return "", "privacy and privkey are required when level is authPriv"
		}
		privacyProto := map[string]string{"aes": "AES", "des": "DES"}[privacy]
		if privacyProto == "" {
			return "", "privacy must be aes or des"
		}
		cmdArgs += " -x " + privacyProto + " -X " + shellQuote(privkey)
	}
	return cmdArgs, ""
}

// snmpParseOutput parses `snmpget`/`snmpwalk -O qn` output — one
// "<numeric-oid> <value>" line per binding (net-snmp's own "quick
// print, numeric OID" format, chosen by this port so output needs no
// local MIB files to be unambiguous) — into a map keyed by OID.
func snmpParseOutput(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		oid, val, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		m[strings.TrimSpace(oid)] = strings.TrimSpace(val)
	}
	return m
}

// snmpTrailingIndex extracts the single trailing dotted component of
// oid past base (e.g. base "1.2.3", oid "1.2.3.7" -> (7, true)).
func snmpTrailingIndex(oid, base string) (int, bool) {
	if !strings.HasPrefix(oid, base+".") {
		return 0, false
	}
	rest := strings.TrimPrefix(oid, base+".")
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// snmpTrailingIP extracts the trailing FOUR dotted components of oid
// past base as a dotted-quad IP string — matching real snmp_facts' own
// `current_oid.rsplit(".", 4)[-4:]`, since the ipAddrTable's own index
// IS the IP address.
func snmpTrailingIP(oid, base string) (string, bool) {
	if !strings.HasPrefix(oid, base+".") {
		return "", false
	}
	rest := strings.TrimPrefix(oid, base+".")
	parts := strings.Split(rest, ".")
	if len(parts) != 4 {
		return "", false
	}
	return strings.Join(parts, "."), true
}

// snmpDecodeHex mirrors real snmp_facts' own decode_hex: a
// "0x..."-prefixed value is hex-decoded into text; anything else (or
// too short to plausibly be "0x" + at least one byte) is returned
// unchanged.
func snmpDecodeHex(s string) string {
	if len(s) < 3 || !strings.HasPrefix(s, "0x") {
		return s
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil {
		return s
	}
	return string(b)
}

// snmpDecodeMAC mirrors real snmp_facts' own decode_mac: a
// "0x"+12-hex-digit value (14 characters total) has its "0x" prefix
// stripped, leaving a bare, colon-free hex string; anything else is
// returned unchanged.
func snmpDecodeMAC(s string) string {
	if len(s) != 14 || !strings.HasPrefix(s, "0x") {
		return s
	}
	return s[2:]
}

func snmpLookupAdminStatus(code string) string {
	switch code {
	case "1":
		return "up"
	case "2":
		return "down"
	case "3":
		return "testing"
	}
	return ""
}

func snmpLookupOperStatus(code string) string {
	switch code {
	case "1":
		return "up"
	case "2":
		return "down"
	case "3":
		return "testing"
	case "4":
		return "unknown"
	case "5":
		return "dormant"
	case "6":
		return "notPresent"
	case "7":
		return "lowerLayerDown"
	}
	return ""
}
