package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// mssqlConnArgs builds the sqlcmd connection flags (-S and, if given,
// -U/-P) shared by mssql_db.go and mssql_script.go, matching the
// login_host/login_port/login_user/login_password options both real
// mssql_db and mssql_script document identically.
//
// Deviation shared by every mssql_* module in this batch: real
// mssql_db.py/mssql_script.py talk to SQL Server through the pymssql
// Python DB-API client (itself built on FreeTDS); this port has no Go
// TDS client wired into remoteexec.Connection, so it substitutes
// shelling out to Microsoft's own `sqlcmd` CLI client on the target
// instead — same observable server-side effect, different transport
// (see redisCli's own doc comment in redis.go for the general
// rationale this port applies to every CLI-backed module).
//
// login_host containing "\\" (a named instance, e.g. `server\instance`)
// is passed to sqlcmd's own -S flag verbatim (sqlcmd understands the
// same `server\instance` syntax pymssql/FreeTDS do); login_port is
// rejected together with a named instance, matching real mssql_db/
// mssql_script's own check. Otherwise, -S is `host,port` (sqlcmd's own
// host,port syntax — note the comma, not pymssql/FreeTDS's colon) when
// login_port is given, or just `host` otherwise (sqlcmd itself
// defaults to port 1433, matching real mssql_db/mssql_script's own
// documented default).
//
// login_user/login_password: if login_user is empty, no -U/-P flags
// are passed at all, letting sqlcmd fall back to its own default
// authentication handling (Windows-integrated auth, or the
// SQLCMDUSER/SQLCMDPASSWORD/SQLCMDSERVER environment variables) —
// real mssql_db/mssql_script instead pass login_user="" through to
// pymssql.connect() itself, whose own behavior in that case depends on
// FreeTDS's configuration; this port cannot reproduce that exactly
// without a FreeTDS install to test against, so it documents the
// difference here rather than guessing. login_password is passed via
// sqlcmd's own -P flag (there is no environment-variable alternative
// for it across sqlcmd versions this port can rely on being present).
func mssqlConnArgs(loginHost, loginPort, loginUser, loginPassword string) ([]string, error) {
	if strings.Contains(loginHost, `\`) && loginPort != "" {
		return nil, errArg("login_port cannot be used with a named instance in login_host (server\\instance format)")
	}
	server := loginHost
	if !strings.Contains(loginHost, `\`) && loginPort != "" {
		server = loginHost + "," + loginPort
	}
	a := []string{"-S", server}
	if loginUser != "" {
		a = append(a, "-U", loginUser, "-P", loginPassword)
	}
	return a, nil
}

// mssqlSqlcmd runs `sqlcmd <connArgs> -b -h -1 -W <extra...>` on the
// target: -b makes sqlcmd exit non-zero (and abort) on the first
// error, -h -1 suppresses column headers, -W trims trailing
// whitespace from fixed-width output — together giving script/query
// output this port's parsing can rely on being just data rows, no
// header/footer noise.
func mssqlSqlcmd(ctx context.Context, conn remoteexec.Connection, connArgs []string, extra ...string) (remoteexec.Result, error) {
	all := append(append([]string{}, connArgs...), "-b", "-h", "-1", "-W")
	all = append(all, extra...)
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = shellQuote(a)
	}
	cmd := "sqlcmd " + strings.Join(quoted, " ")
	return conn.Exec(ctx, cmd, nil)
}

// moduleMssqlDb implements Ansible's `mssql_db` (community.general)
// module: creates or removes a Microsoft SQL Server database, or
// imports a `.sql` dump file into one, via `sqlcmd` (see
// mssqlConnArgs's own doc comment for why, and for the connection
// argument mapping shared with mssql_script.go).
//
// Args: name (required, alias db); login_host (required); login_port;
// login_user (default ""); login_password (default ""); state
// (present|absent|import, default present); target (path, on the
// target, of the `.sql` file to import — required when state=import);
// autocommit (bool, default false — real mssql_db uses this to decide
// whether the DB-API connection commits per-statement (true) or only
// at the very end of the import (false) via `conn.commit()`; this port
// runs the import as one `sqlcmd -i target` invocation per call, which
// has no equivalent partial-commit boundary to gate — sqlcmd commits
// each GO-separated batch as it goes regardless, so autocommit is
// accepted for compatibility but has no effect here, a deviation
// documented rather than silently ignored).
//
// state=present: creates the database if it does not already exist
// (`CREATE DATABASE [name]`); a no-op if it does.
// state=absent: if the database exists, forces it to single-user mode
// (ignoring any error, matching real mssql_db's own bare `except:
// pass`) then `DROP DATABASE [name]`; a no-op if it does not exist.
// state=import: creates the database first if missing (same as
// state=present), then runs `sqlcmd -d name -i target` — letting
// sqlcmd itself handle `GO` batch separation natively, rather than
// this port re-implementing real mssql_db's own manual
// line-by-line "GO" splitting (db_import in the real module works
// around pymssql/FreeTDS not understanding GO at all; sqlcmd, being
// Microsoft's own reference client, needs no such workaround). Fails
// (Fail, not error) if target does not exist on the remote host,
// matching real mssql_db's own `db_import`'s "cannot find target file"
// case.
func moduleMssqlDb(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	db := argString(args, "name", argString(args, "db", ""))
	if db == "" {
		return Result{}, errArg("mssql_db: name (or db) is required")
	}
	loginHost, err := requireString(args, "login_host")
	if err != nil {
		return Result{}, err
	}
	loginPort := argString(args, "login_port", "")
	loginUser := argString(args, "login_user", "")
	loginPassword := argString(args, "login_password", "")
	if loginUser != "" && loginPassword == "" {
		return Result{}, errArg("mssql_db: when supplying login_user, login_password must be provided")
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" && state != "import" {
		return Result{}, errArg("mssql_db: state must be one of present, absent, import, got %q", state)
	}

	connArgs, err := mssqlConnArgs(loginHost, loginPort, loginUser, loginPassword)
	if err != nil {
		return Result{}, err
	}

	exists, err := mssqlDbExists(ctx, conn, connArgs, db)
	if err != nil {
		return Result{}, err
	}

	switch state {
	case "absent":
		if !exists {
			return Ok(db + " already absent"), nil
		}
		_, _ = mssqlSqlcmd(ctx, conn, connArgs, "-Q", "ALTER DATABASE ["+db+"] SET single_user WITH ROLLBACK IMMEDIATE")
		res, err := mssqlSqlcmd(ctx, conn, connArgs, "-Q", "DROP DATABASE ["+db+"]")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("mssql_db: error deleting database: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(db + " dropped"), nil

	case "present":
		if exists {
			return Ok(db + " already present"), nil
		}
		res, err := mssqlSqlcmd(ctx, conn, connArgs, "-Q", "CREATE DATABASE ["+db+"]")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("mssql_db: error creating database: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(db + " created"), nil

	default: // import
		target, err := requireString(args, "target")
		if err != nil {
			return Result{}, errArg("mssql_db: target is required when state=import")
		}
		targetExists, err := pathExists(ctx, conn, target)
		if err != nil {
			return Result{}, err
		}
		if !targetExists {
			return Fail("mssql_db: cannot find target file"), nil
		}
		if !exists {
			res, err := mssqlSqlcmd(ctx, conn, connArgs, "-Q", "CREATE DATABASE ["+db+"]")
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("mssql_db: error creating database: " + strings.TrimSpace(res.Stderr)), nil
			}
		}
		res, err := mssqlSqlcmd(ctx, conn, connArgs, "-d", db, "-i", target)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("mssql_db: import failed: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed("import successful").WithExtra("db", db), nil
	}
}

// mssqlDbExists reports whether db exists via
// `SELECT name FROM master.sys.databases WHERE name = 'db'`, matching
// real mssql_db's own db_exists.
func mssqlDbExists(ctx context.Context, conn remoteexec.Connection, connArgs []string, db string) (bool, error) {
	query := "SET NOCOUNT ON; SELECT name FROM master.sys.databases WHERE name = " + mssqlQuoteLiteral(db)
	res, err := mssqlSqlcmd(ctx, conn, connArgs, "-Q", query)
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, errArg("mssql_db: unable to check database existence: %s", strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout) != "", nil
}

// mssqlQuoteLiteral escapes s for interpolation as a single-quoted
// T-SQL string literal (doubling embedded single quotes).
func mssqlQuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
