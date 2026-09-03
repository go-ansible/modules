package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMssqlScript implements Ansible's `mssql_script`
// (community.general) module: executes a SQL script (optionally split
// into multiple `GO`-separated batches) against a Microsoft SQL Server
// database, via `sqlcmd` (see mssqlConnArgs's own doc comment in
// mssql_db.go for the connection argument mapping shared by both
// mssql_* modules, and for why this port shells out to sqlcmd rather
// than talking to SQL Server directly the way real mssql_script's own
// pymssql client does).
//
// Args: name (alias db, default ""); login_host (required); login_port
// (int); login_user; login_password; script (required); transaction
// (bool, default false); params (dict); output (dict|default, default
// "default" — accepted for compatibility but does not change this
// port's own return shape, see below).
//
// Deviation — no structured query_results/query_results_dict: real
// mssql_script returns each batch's each query's result set as a
// nested list of rows of typed column values
// (query_results[batch][query][row][col]), built from pymssql's own
// DB-API cursor.fetchall(), which returns real typed Python values.
// sqlcmd's own output is a HUMAN-FORMATTED TEXT TABLE (or, with -h -1
// -W, whitespace/newline-delimited text with no column-boundary
// markers when more than one column is selected) with no reliable,
// version-stable way for this port to recover exact column boundaries,
// NULL vs. empty-string, or original SQL types from it — attempting to
// parse it into the exact same nested structure would silently
// misparse in ways a caller could not detect. This port does NOT
// attempt it: it returns the script's combined raw stdout/stderr/rc
// under Extra["stdout"]/["stderr"]/["rc"] instead, and documents this
// gap here rather than faking query_results.
//
// Deviation — params substitution is textual, not bound: real
// mssql_script passes params to pymssql's own cursor.execute(query,
// params), which sends them to the server as genuine bound parameters
// (substituted server-side, not string-interpolated, and therefore not
// vulnerable to SQL injection from the parameter values themselves).
// This port has no bound-parameter mechanism it can drive over a
// single shell command string, so it substitutes each `%(key)s` token
// in script with the corresponding params[key] value, quoted as a
// single-quoted T-SQL string literal (embedded single quotes doubled)
// — a plain textual substitution performed BEFORE the script ever
// reaches sqlcmd. This is NOT injection-safe the way real
// mssql_script's bound parameters are; a caller passing untrusted
// values through params should not rely on this port for the same
// safety guarantee.
//
// Batch splitting: script is split on lines whose trimmed, upper-cased
// content is exactly "GO", matching real mssql_script's own batch
// splitting — but where real mssql_script re-implements that splitting
// in Python (because pymssql/FreeTDS does not understand GO as a
// client-side batch separator), this port could simply hand the whole
// script to sqlcmd as one script and let sqlcmd itself do native GO
// handling; it deliberately keeps the same line-based pre-split
// instead, purely so params substitution and the batch count reported
// in Extra["batches"] are computed the same way regardless.
//
// transaction=true wraps the (params-substituted) script in `BEGIN
// TRANSACTION` / `COMMIT TRANSACTION`, and passes sqlcmd -b (abort on
// first error) so a failing statement leaves the transaction
// uncommitted rather than issuing the COMMIT — approximating real
// mssql_script's own rollback-on-error-under-transaction behavior
// without this port being able to observe pymssql's own per-exception
// classification (real mssql_script treats a "statement has no
// resultset" pymssql OperationalError as non-fatal but any other
// exception as fatal; this port cannot distinguish those from sqlcmd's
// text output, so ANY non-zero sqlcmd exit is treated as a failure —
// Result{Failed:true}, not a Go error, since a bad script is a
// well-formed request that failed at runtime).
//
// check_mode is not modeled by this port at all (no module in this
// package is; see Func's own doc comment in module.go) — real
// mssql_script's own partial check_mode support (skip executing the
// script, still report changed=true) has no equivalent here.
func moduleMssqlScript(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	db := argString(args, "name", argString(args, "db", ""))
	loginHost, err := requireString(args, "login_host")
	if err != nil {
		return Result{}, err
	}
	loginPort := ""
	if v, ok := args["login_port"]; ok {
		loginPort = fmt.Sprint(v)
	}
	loginUser := argString(args, "login_user", "")
	loginPassword := argString(args, "login_password", "")
	if loginUser != "" && loginPassword == "" {
		return Result{}, errArg("mssql_script: when supplying login_user, login_password must be provided")
	}
	script, err := requireString(args, "script")
	if err != nil {
		return Result{}, err
	}
	transaction := argBool(args, "transaction", false)

	params := map[string]string{}
	if raw, ok := args["params"].(map[string]any); ok {
		for k, v := range raw {
			params[k] = fmt.Sprint(v)
		}
	}
	for k, v := range params {
		script = strings.ReplaceAll(script, "%("+k+")s", mssqlQuoteLiteral(v))
	}

	batches := mssqlSplitBatches(script)

	connArgs, err := mssqlConnArgs(loginHost, loginPort, loginUser, loginPassword)
	if err != nil {
		return Result{}, err
	}
	if db != "" {
		connArgs = append(connArgs, "-d", db)
	}

	body := script
	extraFlags := []string{}
	if transaction {
		body = "BEGIN TRANSACTION;\n" + script + "\nIF @@TRANCOUNT > 0 COMMIT TRANSACTION;\n"
		extraFlags = append(extraFlags, "-b")
	}

	res, err := mssqlSqlcmdStdin(ctx, conn, connArgs, body, extraFlags...)
	if err != nil {
		return Result{}, err
	}
	result := Changed("script executed").
		WithExtra("stdout", res.Stdout).
		WithExtra("stderr", res.Stderr).
		WithExtra("rc", res.RC).
		WithExtra("batches", len(batches))
	if res.RC != 0 {
		return Fail("mssql_script: query failed: "+strings.TrimSpace(res.Stderr)).
			WithExtra("stdout", res.Stdout).
			WithExtra("stderr", res.Stderr).
			WithExtra("rc", res.RC), nil
	}
	return result, nil
}

// mssqlSplitBatches splits script on lines whose trimmed, upper-cased
// content is exactly "GO", matching real mssql_script's own
// line-by-line batch splitting.
func mssqlSplitBatches(script string) []string {
	var batches []string
	var cur []string
	for _, line := range strings.SplitAfter(script, "\n") {
		if strings.ToUpper(strings.TrimSpace(line)) == "GO" {
			batches = append(batches, strings.Join(cur, ""))
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 && strings.TrimSpace(strings.Join(cur, "")) != "" {
		batches = append(batches, strings.Join(cur, ""))
	}
	if len(batches) == 0 {
		batches = []string{script}
	}
	return batches
}

// mssqlSqlcmdStdin runs `sqlcmd <connArgs> -b -h -1 -W <extra...>`
// feeding body as its stdin script — used instead of -Q/-i so the
// (possibly multi-batch, GO-containing) script never has to touch a
// temp file on the target.
func mssqlSqlcmdStdin(ctx context.Context, conn remoteexec.Connection, connArgs []string, body string, extra ...string) (remoteexec.Result, error) {
	all := append(append([]string{}, connArgs...), "-h", "-1", "-W")
	all = append(all, extra...)
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = shellQuote(a)
	}
	cmd := "sqlcmd " + strings.Join(quoted, " ")
	return conn.Exec(ctx, cmd, strings.NewReader(body))
}
