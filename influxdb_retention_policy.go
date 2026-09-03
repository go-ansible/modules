package modules

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// influxDurationUnitNanos matches real influxdb_retention_policy.py's
// own DURATION_UNIT_NANOSECS table exactly.
var influxDurationUnitNanos = map[string]float64{
	"ns": 1,
	"u":  1000,
	"µ":  1000,
	"ms": 1000 * 1000,
	"s":  1000 * 1000 * 1000,
	"m":  1000 * 1000 * 1000 * 60,
	"h":  1000 * 1000 * 1000 * 60 * 60,
	"d":  1000 * 1000 * 1000 * 60 * 60 * 24,
	"w":  1000 * 1000 * 1000 * 60 * 60 * 24 * 7,
}

const influxMinimumValidDurationNs = 1 * 1000 * 1000 * 1000 * 60 * 60 // 1h, in nanoseconds

// influxValidDurationRegex/influxDurationRegex/influxExtendedDurationRegex
// match real influxdb_retention_policy.py's own VALID_DURATION_REGEX/
// DURATION_REGEX/EXTENDED_DURATION_REGEX exactly: the plain form only
// accepts integer counts (the shape a user types as an argument); the
// extended form additionally accepts a fractional second count (the
// shape InfluxDB's own SHOW RETENTION POLICIES reports back, e.g.
// "168h0m0s").
var (
	influxValidDurationRegex    = regexp.MustCompile(`^(INF|(\d+(ns|u|µ|ms|s|m|h|d|w)))+$`)
	influxDurationRegex         = regexp.MustCompile(`(\d+)(ns|u|µ|ms|s|m|h|d|w)`)
	influxExtendedDurationRegex = regexp.MustCompile(`(?:(\d+)(ns|u|µ|ms|m|h|d|w)|(\d+(?:\.\d+)?)(s))`)
)

// influxCheckDurationLiteral matches real check_duration_literal.
func influxCheckDurationLiteral(v string) bool {
	return influxValidDurationRegex.MatchString(v)
}

// influxParseDurationLiteral matches real parse_duration_literal: "INF"
// is 0; otherwise every duration_literal is summed in nanoseconds.
func influxParseDurationLiteral(v string, extended bool) float64 {
	if v == "INF" {
		return 0
	}
	re := influxDurationRegex
	if extended {
		re = influxExtendedDurationRegex
	}
	var total float64
	for _, m := range re.FindAllStringSubmatch(v, -1) {
		var numStr, unit string
		if extended {
			if m[1] != "" {
				numStr, unit = m[1], m[2]
			} else {
				numStr, unit = m[3], m[4]
			}
		} else {
			numStr, unit = m[1], m[2]
		}
		n, _ := strconv.ParseFloat(numStr, 64)
		total += n * influxDurationUnitNanos[unit]
	}
	return total
}

// moduleInfluxdbRetentionPolicy implements Ansible's
// `influxdb_retention_policy` (community.general) module: creates,
// alters, or drops an InfluxDB retention policy — read from real
// influxdb_retention_policy.py's own find_retention_policy/
// create_retention_policy/alter_retention_policy/
// drop_retention_policy functions (this batch's hard rule: the exact
// duration-literal validation/parsing and the alter-vs-noop comparison
// are only visible there, not EXAMPLES/OPTIONS).
//
// Args: database_name, policy_name (both required); state
// (present|absent, default present); duration — required (this port's
// own validation; real influxdb_retention_policy.py has no
// required_if for it and would instead crash on Python's re.search
// against a None value) when state=present, must match InfluxDB's own
// duration-literal grammar (an integer count of ns/u/µ/ms/s/m/h/d/w
// units, or "INF"), and — unless "INF"/0 — be at least 1h; replication
// — required (same rationale) when state=present; default (bool,
// default false); shard_group_duration — same grammar/1h-minimum
// constraint as duration, but optional: when omitted on an alter, the
// retention policy's EXISTING shard group duration is left out of the
// comparison entirely (matching real alter_retention_policy's own
// `if shard_group_duration is None: ... = retention_policy[...]`
// short-circuit, which quietly keeps whatever InfluxDB already has
// rather than treating "not specified" as "set to InfluxDB's default").
//
// present = policy_name appears in `SHOW RETENTION POLICIES ON "db"`.
// state=absent drops it if present, otherwise a no-op. state=present:
// if absent, `CREATE RETENTION POLICY`; if present, compares the
// existing policy's duration/shardGroupDuration (both nanosecond-
// parsed via influxParseDurationLiteral(..., extended=true), matching
// real find_retention_policy's own post-processing of InfluxDB's
// reported values) plus replicaN/default against the desired values,
// and only runs `ALTER RETENTION POLICY` if any differ.
func moduleInfluxdbRetentionPolicy(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	database, err := requireString(args, "database_name")
	if err != nil {
		return Result{}, err
	}
	policyName, err := requireString(args, "policy_name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("influxdb_retention_policy: state must be present or absent, got %q", state)
	}

	existing, exists, err := influxFindRetentionPolicy(ctx, conn, args, database, policyName)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if !exists {
			return Ok(""), nil
		}
		return influxRunChange(ctx, conn, args, "influxdb_retention_policy: drop failed",
			"DROP RETENTION POLICY "+influxIdent(policyName)+" ON "+influxIdent(database))
	}

	duration, err := requireString(args, "duration")
	if err != nil {
		return Result{}, errArg("influxdb_retention_policy: duration is required when state=present")
	}
	if !influxCheckDurationLiteral(duration) {
		return Fail("Failed to parse value of duration"), nil
	}
	durationNs := influxParseDurationLiteral(duration, false)
	if durationNs != 0 && durationNs < influxMinimumValidDurationNs {
		return Fail("duration value must be at least 1h"), nil
	}
	if _, ok := args["replication"]; !ok {
		return Result{}, errArg("influxdb_retention_policy: replication is required when state=present")
	}
	replication := argInt(args, "replication", 0)
	defaultPolicy := argBool(args, "default", false)
	shardGroupDuration := argString(args, "shard_group_duration", "")
	shardGiven := shardGroupDuration != ""
	var shardNs float64
	if shardGiven {
		if !influxCheckDurationLiteral(shardGroupDuration) {
			return Fail("Failed to parse value of shard_group_duration"), nil
		}
		shardNs = influxParseDurationLiteral(shardGroupDuration, false)
		if shardNs < influxMinimumValidDurationNs {
			return Fail("shard_group_duration value must be finite and at least 1h"), nil
		}
	}

	if !exists {
		q := fmt.Sprintf("CREATE RETENTION POLICY %s ON %s DURATION %s REPLICATION %d",
			influxIdent(policyName), influxIdent(database), duration, replication)
		if shardGiven {
			q += " SHARD DURATION " + shardGroupDuration
		}
		if defaultPolicy {
			q += " DEFAULT"
		}
		return influxRunChange(ctx, conn, args, "influxdb_retention_policy: create failed", q)
	}

	wantShardNs := existing.shardGroupDurationNs
	if shardGiven {
		wantShardNs = shardNs
	}
	changed := existing.durationNs != durationNs ||
		existing.shardGroupDurationNs != wantShardNs ||
		existing.replicaN != replication ||
		existing.defaultPolicy != defaultPolicy
	if !changed {
		return Ok(""), nil
	}

	q := fmt.Sprintf("ALTER RETENTION POLICY %s ON %s DURATION %s REPLICATION %d",
		influxIdent(policyName), influxIdent(database), duration, replication)
	if shardGiven {
		q += " SHARD DURATION " + shardGroupDuration
	}
	if defaultPolicy {
		q += " DEFAULT"
	}
	return influxRunChange(ctx, conn, args, "influxdb_retention_policy: alter failed", q)
}

type influxRetentionPolicy struct {
	durationNs           float64
	shardGroupDurationNs float64
	replicaN             int
	defaultPolicy        bool
}

func influxFindRetentionPolicy(ctx context.Context, conn remoteexec.Connection, args map[string]any, database, policyName string) (influxRetentionPolicy, bool, error) {
	res, err := influxExecute(ctx, conn, args, "", "SHOW RETENTION POLICIES ON "+influxIdent(database))
	if err != nil {
		return influxRetentionPolicy{}, false, err
	}
	if res.RC != 0 {
		return influxRetentionPolicy{}, false, fmt.Errorf("influxdb_retention_policy: unable to list retention policies: %s", strings.TrimSpace(res.Stderr))
	}
	rows, err := influxRows(res.Stdout)
	if err != nil {
		return influxRetentionPolicy{}, false, fmt.Errorf("influxdb_retention_policy: %w", err)
	}
	for _, row := range rows {
		if fmt.Sprint(row["name"]) != policyName {
			continue
		}
		p := influxRetentionPolicy{
			durationNs:           influxParseDurationLiteral(fmt.Sprint(row["duration"]), true),
			shardGroupDurationNs: influxParseDurationLiteral(fmt.Sprint(row["shardGroupDuration"]), true),
			replicaN:             influxToInt(row["replicaN"]),
			defaultPolicy:        influxToBool(row["default"]),
		}
		return p, true, nil
	}
	return influxRetentionPolicy{}, false, nil
}

func influxToInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func influxToBool(v any) bool {
	b, _ := v.(bool)
	return b
}
