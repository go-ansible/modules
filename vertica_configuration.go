package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleVerticaConfiguration implements Ansible's
// `vertica_configuration` (community.general) module: updates a single
// Vertica configuration parameter via `select
// set_config_parameter(...)`.
//
// Deviation from real vertica_configuration: real vertica_configuration.py
// talks to Vertica through pyodbc; this port shells out to vsql
// instead — see vertica.go's own doc comments for the full rationale
// shared by every vertica_* module in this batch.
//
// Args: parameter (required, alias name); value; db; cluster (default
// localhost); port (default "5433"); login_user (default dbadmin);
// login_password.
//
// Idempotency: reads the parameter's current_value from
// configuration_parameters (filtered to node_name='ALL', matching real
// vertica_configuration's own query) and only calls
// set_config_parameter when it differs from value, case-insensitively
// — matching real vertica_configuration's own
// `current_value.lower() != configuration_facts[...]['current_value'].lower()`
// comparison. An unknown parameter name (no matching row) is reported
// as Fail, matching real vertica_configuration's own KeyError ->
// module.fail_json for that case (its `check()`/`present()` both index
// configuration_facts[parameter_key] unconditionally).
func moduleVerticaConfiguration(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	parameter := argString(args, "parameter", argString(args, "name", ""))
	if parameter == "" {
		return Result{}, errArg("vertica_configuration: parameter (or name) is required")
	}
	value := argString(args, "value", "")

	query := "select parameter_name, current_value from configuration_parameters " +
		"where node_name = 'ALL' and parameter_name ilike " + verticaQuoteLiteral(parameter)
	res, err := verticaVsql(ctx, conn, args, query)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("vertica_configuration: unable to read configuration facts: " + strings.TrimSpace(res.Stderr)), nil
	}
	rows := verticaParseRows(res.Stdout)
	if len(rows) == 0 {
		return Fail("vertica_configuration: unknown parameter " + parameter), nil
	}
	row := rows[0]
	currentValue := ""
	if len(row) > 1 {
		currentValue = row[1]
	}

	if value == "" || strings.EqualFold(value, currentValue) {
		return Ok(parameter + " unchanged"), nil
	}

	setRes, err := verticaVsql(ctx, conn, args, "select set_config_parameter("+verticaQuoteLiteral(parameter)+", "+verticaQuoteLiteral(value)+")")
	if err != nil {
		return Result{}, err
	}
	if setRes.RC != 0 {
		return Fail("vertica_configuration: unable to set " + parameter + ": " + strings.TrimSpace(setRes.Stderr)), nil
	}
	return Changed(parameter + " set to " + value), nil
}
