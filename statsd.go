package modules

import (
	"context"
	"fmt"
	"strconv"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleStatsd implements Ansible's `statsd` (community.general)
// module: sends one counter or gauge metric to a StatsD daemon over
// UDP or TCP.
//
// Architectural note: unlike wakeonlan.go/mqtt.go's own EXAMPLES (both
// exclusively `delegate_to: localhost`, establishing that those two
// send their packet from wherever the Go code itself runs — the
// control node), real statsd's own EXAMPLES carry no delegate_to at
// all. That is the opposite signal: with no delegation, a task runs
// wherever tasks normally run, which for a real Ansible module means
// the copied-over module script executes ON THE TARGET, using
// whatever StatsD Python client is installed there — real statsd's own
// `requirements: statsd` names a target-side dependency, not a
// controller-side one. Consistent with that, this port sends the
// metric from the TARGET by composing the StatsD wire packet
// ("bucket:value|c" or "bucket:value|g") and writing it through
// bash's `/dev/udp/HOST/PORT` or `/dev/tcp/HOST/PORT` pseudo-device
// via conn.Exec — the same "compose a shell one-liner, explicitly
// invoke bash for the pseudo-device" house pattern wait_for.go's own
// doc comment already established, rather than opening a socket
// directly from the Go process the way wakeonlan.go/mqtt.go do.
//
// Args: metric (string, required); metric_type ("counter"|"gauge",
// required); value (int, required); host (default "localhost"); port
// (default 8125); protocol ("udp"|"tcp", default "udp"); metric_prefix
// (default "") — joined to metric with "." on the wire, matching real
// statsd's own StatsClient(prefix=...) behavior (the module's own
// human-readable summary message instead joins prefix and metric with
// "/", which this port also reproduces verbatim as a cosmetic-only
// difference from the wire format); delta (bool, default false) — for
// metric_type=gauge, send value with an explicit leading "+" sign when
// non-negative (a bare negative value already carries its own "-"),
// matching real statsd's own StatsClient.gauge(..., delta=True)
// encoding; state ("present" only, accepted and ignored, matching real
// statsd's own single-choice, do-nothing state option).
//
// Simplifications vs real statsd: `timeout` (only documented as
// meaningful for protocol=tcp) is accepted but not enforced — a
// blocked TCP connect can run for as long as the underlying
// Connection's own Exec allows, rather than failing after `timeout`
// seconds the way real statsd's own TCPStatsClient socket timeout
// would. No check_mode support (real statsd itself has none either:
// check_mode support is "none").
func moduleStatsd(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	metric, err := requireString(args, "metric")
	if err != nil {
		return Result{}, err
	}
	metricType := argString(args, "metric_type", "")
	if metricType != "counter" && metricType != "gauge" {
		return Result{}, errArg("statsd: metric_type must be counter or gauge, got %q", metricType)
	}
	if _, ok := args["value"]; !ok {
		return Result{}, errArg("statsd: missing required argument: value")
	}
	value := argInt(args, "value", 0)
	host := argString(args, "host", "localhost")
	port := argInt(args, "port", 8125)
	protocol := argString(args, "protocol", "udp")
	if protocol != "udp" && protocol != "tcp" {
		return Result{}, errArg("statsd: protocol must be udp or tcp, got %q", protocol)
	}
	metricPrefix := argString(args, "metric_prefix", "")
	delta := argBool(args, "delta", false)

	bucket := metric
	displayName := metric
	if metricPrefix != "" {
		bucket = metricPrefix + "." + metric
		displayName = metricPrefix + "/" + metric
	}

	var valueStr, displayValue, typeSuffix string
	switch metricType {
	case "counter":
		valueStr = strconv.Itoa(value)
		displayValue = valueStr
		typeSuffix = "c"
	default: // gauge
		if delta && value >= 0 {
			valueStr = "+" + strconv.Itoa(value)
		} else {
			valueStr = strconv.Itoa(value)
		}
		displayValue = fmt.Sprintf("%d (delta=%s)", value, boolPyStr(delta))
		typeSuffix = "g"
	}

	payload := fmt.Sprintf("%s:%s|%s", bucket, valueStr, typeSuffix)
	dev := "udp"
	if protocol == "tcp" {
		dev = "tcp"
	}
	inner := fmt.Sprintf("printf %%s %s > /dev/%s/%s/%d", shellQuote(payload), dev, host, port)
	cmd := "bash -c " + shellQuote(inner)

	if _, err := run(ctx, conn, cmd); err != nil {
		return Fail(fmt.Sprintf("Failed sending to StatsD: %v", err)), nil
	}

	return Changed(fmt.Sprintf("Sent %s %s -> %s to StatsD", metricType, displayName, displayValue)), nil
}

// boolPyStr renders b the way real statsd's own f-string interpolation
// of a Python bool prints it ("True"/"False"), since this port's
// display message otherwise quotes real statsd's own message format
// verbatim.
func boolPyStr(b bool) string {
	if b {
		return "True"
	}
	return "False"
}
