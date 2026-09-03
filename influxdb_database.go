package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// influxConnArgs builds the classic `influx` CLI's connection flags
// (-host/-port/-username/-ssl/-unsafeSsl) shared by every influxdb_*.go
// module in this port, matching the hostname/port/username (alias
// login_username)/ssl/validate_certs options every real influxdb_*
// module documents identically via community.general's shared
// _influxdb doc fragment.
func influxConnArgs(args map[string]any) []string {
	a := []string{
		"-host", argString(args, "hostname", "localhost"),
		"-port", strconv.Itoa(argInt(args, "port", 8086)),
		"-username", argAliasString(args, "username", "login_username", "root"),
	}
	if argBool(args, "ssl", false) {
		a = append(a, "-ssl")
	}
	if !argBool(args, "validate_certs", true) {
		a = append(a, "-unsafeSsl")
	}
	return a
}

// argAliasString reads key, falling back to alias if key is absent —
// every real influxdb_* module declares username/password with
// login_username/login_password as an Ansible option alias (the same
// argument reachable under either name); this port's args map is
// looked up under whichever of the two keys the caller actually used.
func argAliasString(args map[string]any, key, alias, def string) string {
	if _, ok := args[key]; ok {
		return argString(args, key, def)
	}
	return argString(args, alias, def)
}

// influxIdent double-quotes s as an InfluxQL identifier, doubling any
// embedded double quote — InfluxQL's own escaping rule for quoted
// identifiers (database/retention-policy/user names).
func influxIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// influxExecute runs `influx <connArgs> [-database DB] -format json
// -execute QUERY` on the target, passing login_password (alias
// password) via the INFLUX_PASSWORD environment variable rather than a
// -password command-line flag — this port's own substitution for the
// classic `influx` CLI's own documented alternative to -password,
// keeping the credential out of the target's process listing (`ps`),
// the same reasoning redis.go's own redisCli documents for
// REDISCLI_AUTH.
//
// Deviation shared by every influxdb_*.go module in this port: real
// influxdb_database/influxdb_query/influxdb_retention_policy/
// influxdb_user/influxdb_write all speak InfluxDB through the Python
// `influxdb` HTTP client library directly (InfluxDBClient), never a
// subprocess. This port has no Go InfluxDB client wired into
// remoteexec.Connection, so it substitutes shelling out to the
// classic (InfluxDB 1.x InfluxQL) `influx` CLI's own -execute flag
// instead — same observable server-side effect, different transport.
// proxies/retries/timeout/udp_port/use_udp/path (real influxdb_*
// modules' own HTTP-client-specific tuning knobs, documented as
// "Only available when using python-influxdb >= N.N.N") have no
// `influx` CLI equivalent and are accepted but silently ignored by
// every influxdb_*.go module in this port.
func influxExecute(ctx context.Context, conn remoteexec.Connection, args map[string]any, database, query string) (remoteexec.Result, error) {
	all := influxConnArgs(args)
	if database != "" {
		all = append(all, "-database", database)
	}
	all = append(all, "-format", "json", "-execute", query)
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = shellQuote(a)
	}
	cmd := "influx " + strings.Join(quoted, " ")
	if pw := argAliasString(args, "password", "login_password", "root"); pw != "" {
		cmd = "INFLUX_PASSWORD=" + shellQuote(pw) + " " + cmd
	}
	return conn.Exec(ctx, cmd, nil)
}

// influxResponse/influxResult/influxSeries decode the classic `influx
// -format json` response body, matching InfluxDB 1.x's own documented
// JSON query response shape.
type influxResponse struct {
	Results []influxResult `json:"results"`
}
type influxResult struct {
	Series []influxSeries `json:"series"`
	Error  string         `json:"error"`
}
type influxSeries struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Values  [][]any  `json:"values"`
}

// influxRows decodes body (an influx -format json response) into a
// flat list of column->value maps, taking the first result's first
// series — every query issued by an influxdb_*.go module in this port
// is a single InfluxQL statement expecting at most one series. A
// result carrying a non-empty "error" field (InfluxQL parsed but
// failed at the server, e.g. dropping a retention policy that does not
// exist) is surfaced as a Go error, not silently dropped.
func influxRows(body string) ([]map[string]any, error) {
	var resp influxResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, fmt.Errorf("decoding influx response %q: %w", body, err)
	}
	if len(resp.Results) == 0 {
		return nil, nil
	}
	if resp.Results[0].Error != "" {
		return nil, fmt.Errorf("%s", resp.Results[0].Error)
	}
	if len(resp.Results[0].Series) == 0 {
		return nil, nil
	}
	series := resp.Results[0].Series[0]
	rows := make([]map[string]any, len(series.Values))
	for i, v := range series.Values {
		row := make(map[string]any, len(series.Columns))
		for j, col := range series.Columns {
			if j < len(v) {
				row[col] = v[j]
			}
		}
		rows[i] = row
	}
	return rows, nil
}

// moduleInfluxdbDatabase implements Ansible's `influxdb_database`
// (community.general) module: creates or drops an InfluxDB database —
// read from real influxdb_database.py's own find_database/
// create_database/drop_database functions.
//
// Args: database_name (required); state (present|absent, default
// present); hostname (default localhost); port (default 8086);
// username (alias login_username, default root); password (alias
// login_password, default root); ssl (default false); validate_certs
// (default true) — see influxExecute's own doc comment for the shared
// influxdb_*.go connection/transport substitution and the client-only
// options this port accepts but cannot act on.
//
// present = database_name appears in `SHOW DATABASES`'s own "name"
// column. state=present creates it (`CREATE DATABASE "name"`) if
// absent, otherwise a no-op; state=absent drops it
// (`DROP DATABASE "name"`) if present, otherwise a no-op — matching
// real influxdb_database's own find_database-then-create/drop shape
// exactly. RETURN VALUES for real influxdb_database document only the
// standard changed/failed/msg fields ("# only defaults"), so this
// module adds no Extra fields of its own.
func moduleInfluxdbDatabase(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "database_name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("influxdb_database: state must be present or absent, got %q", state)
	}

	present, err := influxDatabaseExists(ctx, conn, args, name)
	if err != nil {
		return Result{}, err
	}

	if state == "present" {
		if present {
			return Ok(""), nil
		}
		return influxRunChange(ctx, conn, args, "influxdb_database: create failed", "CREATE DATABASE "+influxIdent(name))
	}

	if !present {
		return Ok(""), nil
	}
	return influxRunChange(ctx, conn, args, "influxdb_database: drop failed", "DROP DATABASE "+influxIdent(name))
}

// influxRunChange runs query (a statement expected to have no
// meaningful result rows) and returns Changed on success or Fail
// prefixed with errPrefix on either a non-zero exit or a server-side
// InfluxQL error.
func influxRunChange(ctx context.Context, conn remoteexec.Connection, args map[string]any, errPrefix, query string) (Result, error) {
	res, err := influxExecute(ctx, conn, args, "", query)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(errPrefix + ": " + strings.TrimSpace(res.Stderr)), nil
	}
	if _, err := influxRows(res.Stdout); err != nil {
		return Fail(errPrefix + ": " + err.Error()), nil
	}
	return Changed(""), nil
}

func influxDatabaseExists(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string) (bool, error) {
	res, err := influxExecute(ctx, conn, args, "", "SHOW DATABASES")
	if err != nil {
		return false, err
	}
	if res.RC != 0 {
		return false, fmt.Errorf("influxdb_database: unable to list databases: %s", strings.TrimSpace(res.Stderr))
	}
	rows, err := influxRows(res.Stdout)
	if err != nil {
		return false, fmt.Errorf("influxdb_database: %w", err)
	}
	for _, row := range rows {
		if fmt.Sprint(row["name"]) == name {
			return true, nil
		}
	}
	return false, nil
}
