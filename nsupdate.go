package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleNsupdate implements Ansible's `nsupdate` (community.general)
// module: creates/updates/removes a DNS record via a dynamic DNS
// (DDNS/RFC 2136) update.
//
// Architectural note (read before assuming this narrows real
// behavior more than it does): real community.general.nsupdate is
// implemented directly against dnspython (dns.update.Update, its own
// TSIG/GSS-TSIG handling, and its own idempotency check — sending a
// PREREQUISITE-only UPDATE message and reading the server's own rcode
// to tell whether the record already has the desired value). This
// port has no DNS protocol library to link against, so — mirroring
// mail.go's own "shell out to a real external tool rather than
// reimplement a protocol in Go" stance — it drives the real
// `nsupdate` binary via its documented stdin command language
// (server/zone/update add/update delete/send), and separately queries
// current record state via `dig` for its own idempotency check (dig
// ships in the same package as nsupdate on every mainstream distro —
// bind-utils/dnsutils). This is NOT the same mechanism as real
// nsupdate's prerequisite-rcode check (see "idempotency" below for
// exactly how it differs), but the same practical goal.
//
// Args: server (string, required) — passed as nsupdate's own `server`
// command (with port, if not 53); port (int, default 53); record
// (string, required) — must end with "." (absolute) when zone is
// unset, matching real nsupdate's own restriction (real nsupdate's own
// dnspython-based zone auto-lookup needs an unambiguous name; this
// port applies the identical restriction rather than relying on the
// `nsupdate` binary's own less-well-specified default-origin
// behavior); zone (string, optional) — when set, passed as nsupdate's
// own `zone` command (its own trailing "." is added if missing); when
// unset, no `zone` command is sent at all, letting `nsupdate` itself
// perform the real SOA-based zone lookup (its own documented
// behavior) instead of this port reimplementing that walk-up-the-
// name-tree logic in Go; type (string, default "A"); ttl (int,
// default 3600); value ([]string, required when state=present) — for
// type=TXT, each entry is quoted if not already (matching real
// nsupdate's own txt_helper); state (present|absent, default
// "present"); protocol (tcp|udp, default "tcp") — tcp adds nsupdate's
// own `-v` (force TCP) flag; udp omits it, which asks nsupdate to try
// UDP first the way it does by default — NOT a strict UDP-only
// guarantee the way real nsupdate's own dnspython dns.query.udp call
// is (a real, documented narrowing: nsupdate itself can still fall
// back to TCP on truncation even with `-v` omitted); timeout (float,
// default 10) — truncated to an int second count for nsupdate's own
// `-t` flag (nsupdate has no sub-second timeout option); key_name,
// key_secret, key_algorithm (default "hmac-md5") — passed as
// nsupdate's own `-y <algorithm>:<key_name>:<key_secret>`
// (HMAC-MD5.SIG-ALG.REG.INT and hmac-md5 both map to nsupdate's own
// "hmac-md5" token); key_algorithm=gss-tsig instead adds nsupdate's
// own `-g` flag (GSS-TSIG) and requires key_name/key_secret to be
// unset (matching real nsupdate's own validation) — like real
// nsupdate, this depends on Kerberos credentials already being
// available in the environment `nsupdate` itself runs in; this port
// has no gssapi library of its own to perform the negotiation real
// nsupdate's own init_gssapi does, so it is exactly as dependent on
// pre-existing Kerberos state as the real `nsupdate` CLI itself is.
//
// Idempotency: queries current records for the target name+type via
// `dig +noall +answer`, parsing each answer line's TTL and rdata.
// state=absent is unchanged if the query returns nothing; state=present
// is unchanged if the returned (ttl, value) set exactly matches the
// requested one (order-independent, but an EXACT string match per
// value — this can differ from real nsupdate's own DNS-protocol-level
// comparison for record types whose textual form varies, e.g. a
// resolver normalizing case or trailing dots; a false "changed" in
// that case still applies the identical, correct update, so it is a
// reporting-only gap, never a correctness one, matching this
// package's "best-effort, documented" convention used elsewhere, e.g.
// supervisorctl.go's own status-line check). If `dig` itself is
// missing or its query fails, this port conservatively reports
// Changed and applies the update rather than guessing.
//
// A record update always deletes then re-adds the full requested
// value set for state=present (a full recordset REPLACE, matching
// real nsupdate's own modify_record for every type EXCEPT NS — real
// nsupdate special-cases NS to avoid a documented BIND9 safety refusal
// ("attempt to delete all SOA or NS records ignored") by diffing and
// deleting only the no-longer-wanted NS entries; this port does not
// replicate that NS-specific dance, so deleting every existing NS
// record for a name in one nsupdate call may itself be refused by
// BIND — a real, visible nsupdate failure (Result{Failed:true}), never
// a silent one).
//
// Return value differs from real nsupdate's own dns_rc/dns_rc_str
// (dnspython's own numeric+text response code, which this port has no
// way to produce honestly without dnspython): Extra["rc"] is instead
// `nsupdate`'s own process exit code (0 on success).
func moduleNsupdate(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	server, err := requireString(args, "server")
	if err != nil {
		return Result{}, err
	}
	record, err := requireString(args, "record")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("nsupdate: state must be present or absent, got %q", state)
	}
	port := argInt(args, "port", 53)
	keyName := argString(args, "key_name", "")
	keySecret := argString(args, "key_secret", "")
	keyAlgorithm := argString(args, "key_algorithm", "hmac-md5")
	zone := argString(args, "zone", "")
	rtype := argString(args, "type", "A")
	ttl := argInt(args, "ttl", 3600)
	values := argStringList(args, "value")
	protocol := argString(args, "protocol", "tcp")
	timeout := argInt(args, "timeout", 10)

	if keyAlgorithm == "gss-tsig" && keyName != "" {
		return Result{}, errArg("nsupdate: key_name cannot be used with gss-tsig")
	}

	if zone == "" {
		if !strings.HasSuffix(record, ".") {
			return Result{}, errArg("nsupdate: record must be absolute (end with '.') when zone is omitted")
		}
	} else if !strings.HasSuffix(zone, ".") {
		zone += "."
	}
	fqdn := record
	if !strings.HasSuffix(record, ".") {
		fqdn = record + "." + zone
	}

	if state == "present" && len(values) == 0 {
		return Result{}, errArg("nsupdate: value is required when state is present")
	}
	values = nsupdateTxtWrap(rtype, values)

	changed := true
	if digRes, err := runStatus(ctx, conn, "command -v dig >/dev/null 2>&1"); err == nil && digRes.RC == 0 {
		if current, ok := nsupdateQuery(ctx, conn, server, port, protocol, fqdn, rtype); ok {
			if state == "absent" {
				changed = len(current) > 0
			} else {
				changed = !nsupdateValuesMatch(current, values, ttl)
			}
		}
	}

	recordFact := map[string]any{"zone": zone, "record": record, "type": rtype, "ttl": ttl, "value": values}
	if !changed {
		return Ok(fqdn+" unchanged").WithExtra("record", recordFact).WithExtra("rc", 0), nil
	}

	var script strings.Builder
	serverLine := "server " + server
	if port != 53 {
		serverLine += " " + strconv.Itoa(port)
	}
	script.WriteString(serverLine + "\n")
	if zone != "" {
		script.WriteString("zone " + zone + "\n")
	}
	script.WriteString("update delete " + record + " " + rtype + "\n")
	if state == "present" {
		for _, v := range values {
			script.WriteString("update add " + record + " " + strconv.Itoa(ttl) + " " + rtype + " " + v + "\n")
		}
	}
	script.WriteString("send\n")

	cmd := "nsupdate"
	switch {
	case keyAlgorithm == "gss-tsig":
		cmd += " -g"
	case keyName != "":
		cmd += " -y " + shellQuote(nsupdateAlgorithmToken(keyAlgorithm)+":"+keyName+":"+keySecret)
	}
	if protocol == "tcp" {
		cmd += " -v"
	}
	cmd += " -t " + strconv.Itoa(timeout)

	res, err := conn.Exec(ctx, cmd, strings.NewReader(script.String()))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail("nsupdate: "+msg).WithExtra("rc", res.RC), nil
	}

	verb := "updated"
	if state == "absent" {
		verb = "removed"
	}
	return Changed(fqdn+" "+verb).WithExtra("record", recordFact).WithExtra("rc", 0), nil
}

// nsupdateAlgorithmToken maps a key_algorithm value onto the token
// nsupdate's own `-y` flag expects.
func nsupdateAlgorithmToken(alg string) string {
	if alg == "HMAC-MD5.SIG-ALG.REG.INT" {
		return "hmac-md5"
	}
	return alg
}

// nsupdateTxtWrap quotes each value for type=TXT (case-insensitive),
// matching real nsupdate's own txt_helper; every other type is
// returned unchanged.
func nsupdateTxtWrap(rtype string, values []string) []string {
	if !strings.EqualFold(rtype, "TXT") {
		return values
	}
	out := make([]string, len(values))
	for i, v := range values {
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			out[i] = v
		} else {
			out[i] = `"` + v + `"`
		}
	}
	return out
}

type nsupdateRecordEntry struct {
	ttl   int
	value string
}

// nsupdateQuery runs `dig +noall +answer` for fqdn/rtype against
// server:port and parses each answer line's TTL and rdata. ok is false
// if the query itself failed (missing dig, network error, etc.) — the
// caller falls back to a conservative "assume changed".
func nsupdateQuery(ctx context.Context, conn remoteexec.Connection, server string, port int, protocol, fqdn, rtype string) (entries []nsupdateRecordEntry, ok bool) {
	cmd := "dig +noall +answer"
	if protocol == "tcp" {
		cmd += " +tcp"
	}
	cmd += " @" + shellQuote(server) + " -p " + strconv.Itoa(port) + " " + shellQuote(fqdn) + " " + shellQuote(rtype)
	res, err := runStatus(ctx, conn, cmd)
	if err != nil || res.RC != 0 {
		return nil, false
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		ttl, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		entries = append(entries, nsupdateRecordEntry{ttl: ttl, value: strings.Join(fields[4:], " ")})
	}
	return entries, true
}

// nsupdateValuesMatch reports whether current already has exactly
// wantValues (as a multiset, order-independent) all at wantTTL.
func nsupdateValuesMatch(current []nsupdateRecordEntry, wantValues []string, wantTTL int) bool {
	if len(current) != len(wantValues) {
		return false
	}
	remaining := map[string]int{}
	for _, c := range current {
		if c.ttl != wantTTL {
			return false
		}
		remaining[c.value]++
	}
	for _, v := range wantValues {
		if remaining[v] <= 0 {
			return false
		}
		remaining[v]--
	}
	return true
}
