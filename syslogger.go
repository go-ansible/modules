package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSyslogger implements Ansible's `syslogger` (community.general)
// module: adds one log entry to the target's syslog.
//
// Architectural note: real syslogger's own module calls Python's
// `syslog` standard-library module directly (openlog/syslog/closelog),
// which talks to the local host's syslog — i.e. wherever the module
// itself executes, the target in real Ansible's normal (non-delegated)
// case. This port has no target-side syscall access, so it composes
// the equivalent `logger` command (util-linux's/BSD's own syslog CLI)
// and runs it via conn.Exec, matching this batch's own task
// description ("sends a message to the system log via the `logger`
// command").
//
// Args: msg (string, required); priority (string, default "info" —
// one of emerg, alert, crit, err, warning, notice, info, debug, mapped
// to `logger -p facility.priority`); facility (string, default
// "daemon" — one of kern, user, mail, daemon, auth, lpr, news, uucp,
// cron, syslog, local0-local7); log_pid (bool, default false) —
// `logger -i`; ident (string, default "ansible_syslogger") —
// `logger -t`.
//
// Deviation from real syslogger: real syslogger's own log_pid
// (syslog.LOG_PID passed to openlog()) makes each message carry the
// PID of the process that is ITSELF making the syslog() call — i.e.
// the real Python module's own PID. This port's `logger -i` instead
// logs the PID of the separately-forked `logger` process, which is
// never the same PID (there is no way to make a message carry "the
// PID of the code that decided to log it" when that decision is made
// by a different process than the one doing the logging, which is
// inherent to shelling out rather than calling syslog() in-process).
// Portability: `logger`'s exact flag set (in particular `-i` and
// honoring `--` as an end-of-options marker) is a util-linux/BSD
// convention, not POSIX-guaranteed on every `logger` implementation
// this port's target Connections might reach.
//
// Always reports Changed on success (real syslogger's own module has
// no idempotency concept — a syslog write is inherently a one-way
// append) and never supports check_mode, matching real syslogger's
// own check_mode support: "none".
func moduleSyslogger(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	msg, err := requireString(args, "msg")
	if err != nil {
		return Result{}, err
	}
	priority := argString(args, "priority", "info")
	switch priority {
	case "emerg", "alert", "crit", "err", "warning", "notice", "info", "debug":
	default:
		return Result{}, errArg("syslogger: priority must be one of emerg, alert, crit, err, warning, notice, info, debug, got %q", priority)
	}
	facility := argString(args, "facility", "daemon")
	switch facility {
	case "kern", "user", "mail", "daemon", "auth", "lpr", "news", "uucp", "cron", "syslog",
		"local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7":
	default:
		return Result{}, errArg("syslogger: facility must be a valid syslog facility, got %q", facility)
	}
	logPid := argBool(args, "log_pid", false)
	ident := argString(args, "ident", "ansible_syslogger")

	tokens := []string{"logger", "-p", facility + "." + priority, "-t", ident}
	if logPid {
		tokens = append(tokens, "-i")
	}
	tokens = append(tokens, "--", msg)
	cmd := quoteAll(tokens)

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}

	r := Result{
		Extra: map[string]any{
			"ident": ident, "priority": priority, "facility": facility,
			"log_pid": logPid, "msg": msg,
		},
	}
	if res.RC != 0 {
		r.Failed = true
		r.Msg = fmt.Sprintf("Failed to write to syslog: %s", strings.TrimSpace(res.Stderr))
		return r, nil
	}
	r.Changed = true
	return r, nil
}
