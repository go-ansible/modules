package modules

import (
	"context"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// verticaConnArgs builds the vsql connection flags (-h/-p/-U and -d if a
// database is given) shared by every vertica_* module in this batch,
// matching the cluster/port/login_user/db options every real
// vertica_* (community.general) module documents identically. Real
// vertica_* modules connect through pyodbc (a Vertica ODBC DSN); this
// port has no ODBC driver wired into remoteexec.Connection, so it
// substitutes shelling out to Vertica's own `vsql` CLI client on the
// target instead — same observable server-side effect (DDL/DML issued
// against the same catalog tables real vertica_* queries), different
// transport. Every vertica_*.go file in this batch documents this same
// substitution rather than repeating it.
func verticaConnArgs(args map[string]any) []string {
	a := []string{
		"-h", argString(args, "cluster", "localhost"),
		"-p", argString(args, "port", "5433"),
		"-U", argString(args, "login_user", "dbadmin"),
	}
	if db := argString(args, "db", ""); db != "" {
		a = append(a, "-d", db)
	}
	return a
}

// verticaVsql runs `vsql <connArgs> -X -A -t -c <query>` on the target:
// -X ignores any vsqlrc startup file, -A selects unaligned output
// (fields separated by a bare '|', no padding), -t suppresses column
// headers and the trailing "(N rows)" footer — together giving
// cleanly parseable `field|field|...` output lines, one per result
// row. login_password (if set) is passed via the VSQL_PASSWORD
// environment variable rather than a command-line flag — vsql's own
// documented non-interactive password mechanism (mirroring psql's
// PGPASSWORD, which vsql is forked from), keeping the password out of
// the target's process listing (`ps`). It is still embedded in the
// single shell command string handed to Connection.Exec, an
// architectural limit of this port shared with every other CLI-backed
// module (see redisCli's own doc comment in redis.go for the general
// case).
func verticaVsql(ctx context.Context, conn remoteexec.Connection, args map[string]any, query string) (remoteexec.Result, error) {
	all := append(verticaConnArgs(args), "-X", "-A", "-t", "-c", query)
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = shellQuote(a)
	}
	cmd := "vsql " + strings.Join(quoted, " ")
	if pw := argString(args, "login_password", ""); pw != "" {
		cmd = "VSQL_PASSWORD=" + shellQuote(pw) + " " + cmd
	}
	return conn.Exec(ctx, cmd, nil)
}

// verticaQuoteLiteral escapes s for interpolation as a single-quoted
// SQL string literal (doubling embedded single quotes), the same way
// every vertica_* module in this batch builds its DDL/DML text — vsql
// has no client-side bound-parameter mechanism this port can drive
// over a single Exec command string, so (like real vertica_*'s own
// f-string-built `create role {role}` etc. for identifiers) values are
// interpolated directly rather than bound.
func verticaQuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// verticaSplitList splits a comma-separated module argument (e.g.
// assigned_roles="role_a,role_b") the same way every vertica_*
// module's own Python does: `s.split(",")` filtered to drop empty
// entries, WITHOUT trimming whitespace around each entry — a real,
// intentional-looking quirk of the module this being ported preserves
// rather than "fixes": `"role_a, role_b"` yields `["role_a", " role_b"]`,
// the leading space included, in both the original and this port.
func verticaSplitList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// verticaParseCatalogList parses a catalog column holding a
// comma-separated list (e.g. users.all_roles) the way every vertica_*
// module's own Python does: `s.replace(" ", "").split(",")` — every
// space removed first (not just leading/trailing), then split on
// comma. Returns nil for an empty string, matching the real modules'
// own `if row.all_roles:` guard before calling this.
func verticaParseCatalogList(s string) []string {
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// verticaSameSet reports whether a and b contain the same elements,
// ignoring order but not duplicates or case — matching every vertica_*
// module's own `sorted(a) != sorted(b)` comparisons.
func verticaSameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// verticaParseRows splits vsql's own `-A -t` output ("field|field|...",
// one result row per line) into rows of fields; a trailing blank line
// (vsql's own final newline) is dropped.
func verticaParseRows(stdout string) [][]string {
	stdout = strings.TrimRight(stdout, "\n")
	if stdout == "" {
		return nil
	}
	var rows [][]string
	for _, line := range strings.Split(stdout, "\n") {
		rows = append(rows, strings.Split(line, "|"))
	}
	return rows
}
