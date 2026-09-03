package modules

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleInfluxdbWrite implements Ansible's `influxdb_write`
// (community.general) module: writes data points into InfluxDB — read
// from real influxdb_write.py's own AnsibleInfluxDBWrite.write_data_point,
// which is a thin wrapper around python-influxdb's own
// InfluxDBClient.write_points(data_points).
//
// Args: data_points ([]map, required) — each entry needs "measurement"
// (required) and "fields" (required, a non-empty dict); "tags" (dict,
// optional) and "time" (optional — either an RFC3339 timestamp string,
// matching real influxdb_write's own EXAMPLES use of
// `ansible_date_time.iso8601`, or an already-nanoseconds integer) are
// both optional; database_name (required); hostname, port, username
// (alias login_username), password (alias login_password), ssl,
// validate_certs — see influxdb_database.go's own influxExecute doc
// comment for the shared influxdb_*.go connection/transport
// substitution.
//
// Each data point is encoded as one InfluxDB line-protocol line (tags
// and fields both emitted in sorted key order for determinism — real
// write_points via the requests-based HTTP client has no such
// ordering guarantee, since it iterates a Python dict) and sent as its
// own `INSERT <line>` statement via influxExecute.
//
// Deviation: real influxdb_write batches every point into a SINGLE
// HTTP POST via write_points (all-or-nothing at the transport level).
// This port has no batched INSERT available through the `influx` CLI's
// own -execute flag (see influxdb_database.go's own influxExecute doc
// comment on why a CLI substitution is used at all), so it issues one
// INSERT per point instead; a failure partway through leaves earlier
// points already written — Fail's own message names which data_points
// index failed so a caller can tell which points landed.
//
// Always reports Changed=true on success, matching real influxdb_write's
// own unconditional `module.exit_json(changed=True)`.
func moduleInfluxdbWrite(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	database, err := requireString(args, "database_name")
	if err != nil {
		return Result{}, err
	}
	rawPoints, ok := args["data_points"].([]any)
	if !ok || len(rawPoints) == 0 {
		return Result{}, errArg("influxdb_write: data_points is required")
	}

	for i, rp := range rawPoints {
		point, ok := rp.(map[string]any)
		if !ok {
			return Result{}, errArg("influxdb_write: data_points[%d] must be a dict", i)
		}
		line, err := influxLineProtocol(point)
		if err != nil {
			return Result{}, err
		}
		res, err := influxExecute(ctx, conn, args, database, "INSERT "+line)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(fmt.Sprintf("influxdb_write: writing data_points[%d] failed: %s", i, strings.TrimSpace(res.Stderr))), nil
		}
		if _, err := influxRows(res.Stdout); err != nil {
			return Fail(fmt.Sprintf("influxdb_write: writing data_points[%d] failed: %s", i, err.Error())), nil
		}
	}
	return Changed(""), nil
}

// influxLineProtocol encodes one data_points entry as an InfluxDB
// line-protocol line: measurement[,tag=val...] field=val[,field=val...]
// [timestamp].
func influxLineProtocol(point map[string]any) (string, error) {
	measurement, err := requireString(point, "measurement")
	if err != nil {
		return "", errArg("influxdb_write: data_points[].measurement is required")
	}

	var b strings.Builder
	b.WriteString(influxEscapeMeasurement(measurement))

	if tags, ok := point["tags"].(map[string]any); ok {
		for _, k := range influxSortedKeys(tags) {
			b.WriteString(",")
			b.WriteString(influxEscapeTagKV(k))
			b.WriteString("=")
			b.WriteString(influxEscapeTagKV(fmt.Sprint(tags[k])))
		}
	}

	fields, _ := point["fields"].(map[string]any)
	if len(fields) == 0 {
		return "", errArg("influxdb_write: data_points[].fields is required and must be non-empty")
	}
	b.WriteString(" ")
	for i, k := range influxSortedKeys(fields) {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(influxEscapeTagKV(k))
		b.WriteString("=")
		fv, err := influxFieldValue(fields[k])
		if err != nil {
			return "", err
		}
		b.WriteString(fv)
	}

	if t, ok := point["time"]; ok {
		ns, err := influxTimeToNanos(t)
		if err != nil {
			return "", err
		}
		b.WriteString(" ")
		b.WriteString(strconv.FormatInt(ns, 10))
	}

	return b.String(), nil
}

func influxSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// influxFieldValue encodes a field value per InfluxDB line protocol:
// bool -> t/f, int -> "<n>i", float -> a plain decimal, string ->
// double-quoted with `"`/`\` escaped.
func influxFieldValue(v any) (string, error) {
	switch n := v.(type) {
	case bool:
		if n {
			return "t", nil
		}
		return "f", nil
	case int:
		return strconv.Itoa(n) + "i", nil
	case int64:
		return strconv.FormatInt(n, 10) + "i", nil
	case float64:
		return strconv.FormatFloat(n, 'g', -1, 64), nil
	case string:
		escaped := strings.ReplaceAll(n, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`, nil
	default:
		return "", errArg("influxdb_write: unsupported field value type %T", v)
	}
}

// influxEscapeMeasurement backslash-escapes comma and space, InfluxDB
// line protocol's own required escaping for a measurement name.
func influxEscapeMeasurement(s string) string {
	s = strings.ReplaceAll(s, `,`, `\,`)
	s = strings.ReplaceAll(s, ` `, `\ `)
	return s
}

// influxEscapeTagKV backslash-escapes comma, equals, and space,
// InfluxDB line protocol's own required escaping for a tag/field key
// or an unquoted tag value.
func influxEscapeTagKV(s string) string {
	s = strings.ReplaceAll(s, `,`, `\,`)
	s = strings.ReplaceAll(s, `=`, `\=`)
	s = strings.ReplaceAll(s, ` `, `\ `)
	return s
}

// influxTimeToNanos accepts either an already-nanoseconds integer or
// an RFC3339 timestamp string (matching real influxdb_write's own
// EXAMPLES use of `ansible_date_time.iso8601`) and returns nanoseconds
// since the Unix epoch.
func influxTimeToNanos(v any) (int64, error) {
	switch t := v.(type) {
	case int:
		return int64(t), nil
	case int64:
		return t, nil
	case float64:
		return int64(t), nil
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return 0, errArg("influxdb_write: data_points[].time %q is not a valid RFC3339 timestamp: %v", t, err)
		}
		return parsed.UnixNano(), nil
	default:
		return 0, errArg("influxdb_write: unsupported time value type %T", v)
	}
}
