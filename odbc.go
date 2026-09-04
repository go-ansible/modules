package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOdbc implements Ansible's `odbc` (community.general) module:
// runs a SQL query against an ODBC data source via `isql`, unixODBC's
// own interactive SQL CLI, instead of real odbc's own Python `pyodbc`
// DB-API client — matching this batch's own CLI-substitution pattern,
// the same reasoning mssql_script.go/vertica.go already document for
// substituting `sqlcmd`/`vsql` for their own DB-API clients (pymssql/
// pyodbc-over-a-Vertica-DSN respectively).
//
// Args: dsn (required) — real odbc's own `dsn` is whatever string
// pyodbc.connect() accepts, which includes BOTH a plain named DSN
// (looked up in odbc.ini) and a full "DRIVER=...;Server=...;..."
// driver connection string (real odbc's own EXAMPLES uses the latter
// form). `isql`'s own default first-argument form only accepts a
// NAMED DSN (it calls SQLConnect, not SQLDriverConnect) — this port
// always passes `isql -k <dsn>`, unixODBC's own documented flag that
// switches `isql` to SQLDriverConnect instead, accepting a full
// connection string the same way pyodbc.connect() does; a plain named
// DSN string is also still accepted as a (degenerate, single-keyword)
// connection string under `-k`, so this port's single code path covers
// both of real odbc's own accepted dsn forms. query (required); params
// ([]string, optional); commit (bool, default true).
//
// Deviation — params substitution is textual, not bound: real odbc
// passes params to pyodbc's own cursor.execute(query, params), sent to
// the server as genuine bound parameters. `isql` has no client-side
// bound-parameter mechanism this port can drive over a single shell
// command string; this port instead substitutes each `?` placeholder
// in query, IN ORDER, with the corresponding params[i] value quoted as
// a single-quoted SQL string literal (embedded single quotes doubled)
// — the same textual-substitution tradeoff mssql_script.go's own doc
// comment already accepts for the same architectural reason, and NOT
// injection-safe the way real odbc's bound parameters are.
//
// Deviation — commit has no effect: `isql` has no client-side
// transaction-control flag comparable to pyodbc's cursor.commit()/
// autocommit mode — whether a statement is auto-committed is governed
// by the underlying ODBC DRIVER's own default behavior (most operate
// autocommit-on by default outside an explicit transaction), not
// anything `isql` itself controls. This port accepts `commit` for
// argument-shape compatibility but does not act on it — a documented,
// deliberate narrowing, not a silent gap.
//
// Deviation — no structured results/description: real odbc's own
// results/description/row_count return values are built from pyodbc's
// own typed DB-API cursor (cursor.fetchall(), cursor.description,
// cursor.rowcount) — real typed Python values with reliable column
// boundaries. `isql`'s own output is a human-formatted, driver-and-
// version-dependent text table with no portable, safely-parseable
// column-boundary/NULL-vs-empty-string/type signal this port could
// recover without guessing (this project's own hard rule: don't guess
// at behavior this port cannot verify) — so, matching mssql_script.go's
// own identical, explicitly documented gap for the same underlying
// reason, this port does NOT attempt to parse `isql`'s output into
// Extra["results"]/["description"]/["row_count"] at all. It returns
// the query's raw combined stdout/stderr/rc under Extra["stdout"]/
// ["stderr"]/["rc"] instead.
//
// Matching real odbc's own documented note ("this module always
// returns changed=true whether or not the query would change the
// database" — a `command`-module-like contract, not a genuine idempotency
// check), this port always reports Changed=true on a successful run.
func moduleOdbc(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	dsn, err := requireString(args, "dsn")
	if err != nil {
		return Result{}, err
	}
	query, err := requireString(args, "query")
	if err != nil {
		return Result{}, err
	}
	params := argStringList(args, "params")

	query = odbcSubstituteParams(query, params)

	cmd := "isql -b -k " + shellQuote(dsn)
	res, err := conn.Exec(ctx, cmd, strings.NewReader(query+"\n"))
	if err != nil {
		return Result{}, err
	}

	result := Result{Changed: true, Failed: res.RC != 0}
	if result.Failed {
		result.Msg = "odbc: failed to execute query: " + strings.TrimSpace(res.Stderr)
	}
	result = result.WithExtra("stdout", res.Stdout)
	result = result.WithExtra("stderr", res.Stderr)
	result = result.WithExtra("rc", res.RC)
	return result, nil
}

// odbcSubstituteParams replaces each `?` placeholder in query, in
// order, with the corresponding params[i] value quoted as a
// single-quoted SQL literal — see moduleOdbc's own doc comment on why
// this is textual, not bound.
func odbcSubstituteParams(query string, params []string) string {
	if len(params) == 0 {
		return query
	}
	var b strings.Builder
	pi := 0
	for _, r := range query {
		if r == '?' && pi < len(params) {
			b.WriteString(odbcQuoteLiteral(params[pi]))
			pi++
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// odbcQuoteLiteral escapes s for interpolation as a single-quoted SQL
// string literal, doubling embedded single quotes — the same
// convention verticaQuoteLiteral (vertica.go) already uses.
func odbcQuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
