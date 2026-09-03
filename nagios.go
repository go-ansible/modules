package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// nagiosCfgLocations are the nagios.cfg/icinga.cfg paths this port
// probes, in order, to auto-detect the command file when `cmdfile` is
// not given — the exact list (and order) real nagios.py's own
// which_cmdfile() checks.
var nagiosCfgLocations = []string{
	"/etc/nagios/nagios.cfg",
	"/etc/nagios3/nagios.cfg",
	"/etc/nagios2/nagios.cfg",
	"/usr/local/etc/nagios/nagios.cfg",
	"/usr/local/groundwork/nagios/etc/nagios.cfg",
	"/omd/sites/oppy/tmp/nagios/nagios.cfg",
	"/usr/local/nagios/etc/nagios.cfg",
	"/usr/local/nagios/nagios.cfg",
	"/opt/nagios/etc/nagios.cfg",
	"/opt/nagios/nagios.cfg",
	"/etc/icinga/icinga.cfg",
	"/usr/local/icinga/etc/icinga.cfg",
}

// moduleNagios implements Ansible's `nagios` (community.general) module:
// schedules downtime, acknowledges problems, forces checks, and toggles
// notifications by writing one or more Nagios "external command" lines
// to Nagios's own command-file FIFO — a pure local file write, matching
// real nagios's own documented behavior ("does not use Nagios' HTTP
// API"; must run directly on the Nagios server).
//
// Args: action (required — one of downtime, delete_downtime, silence,
// unsilence, enable_alerts, disable_alerts, silence_nagios,
// unsilence_nagios, command, servicegroup_host_downtime,
// servicegroup_service_downtime, acknowledge, forced_check); host;
// services (aliased service; a single-element list of "host" or "all"
// is a special sentinel, matching real nagios's own Nagios.__init__ —
// "host" scopes the action to the host itself, "all" to every service
// on that host); servicegroup; author (default "Ansible"); comment
// (default "Scheduling downtime"); minutes (default 30); start (epoch
// seconds, as a string); command (raw command text, required when
// action=command); cmdfile — path to the command-file FIFO; when unset,
// this port probes nagiosCfgLocations in order for a `command_file =
// ...` line, matching real nagios's own which_cmdfile(), and fails
// cleanly if none is found (a Result{Failed:true}, since a target
// missing every candidate nagios.cfg is a normal, expected outcome, not
// a transport problem — see module.go's own doc comment on when this
// package uses an error instead).
//
// Every external-command line is written verbatim in Nagios's own
// documented external-command syntax (see
// http://old.nagios.org/developerinfo/externalcommands/commandlist.php
// and each nagiosFmt* helper's own doc comment below); Extra["commands"]
// lists every line written, matching real nagios's own `nagios_commands`
// return value.
//
// Like real nagios, this module is NOT idempotent — every action always
// reports Changed on success, since Nagios's own command-file protocol
// is fire-and-forget (there is nothing to read back and compare).
func moduleNagios(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	action, err := requireString(args, "action")
	if err != nil {
		return Result{}, err
	}
	author := argString(args, "author", "Ansible")
	comment := argString(args, "comment", "Scheduling downtime")
	host := argString(args, "host", "")
	servicegroup := argString(args, "servicegroup", "")
	minutes := argInt(args, "minutes", 30)
	rawCommand := argString(args, "command", "")

	var start *int64
	if s := argString(args, "start", ""); s != "" {
		v, perr := strconv.ParseInt(s, 10, 64)
		if perr != nil {
			return Result{}, errArg("nagios: start must be an integer (epoch seconds), got %q", s)
		}
		start = &v
	}

	servicesList := argStringList(args, "services")
	if servicesList == nil {
		servicesList = argStringList(args, "service")
	}
	svcMode := ""
	if len(servicesList) == 1 && (servicesList[0] == "host" || servicesList[0] == "all") {
		svcMode = servicesList[0]
		servicesList = nil
	}

	switch action {
	case "downtime", "delete_downtime", "enable_alerts", "disable_alerts", "acknowledge", "forced_check":
		if host == "" || (svcMode == "" && len(servicesList) == 0) {
			return Result{}, errArg("nagios: action=%s requires host and services", action)
		}
	case "silence", "unsilence":
		if host == "" {
			return Result{}, errArg("nagios: action=%s requires host", action)
		}
	case "command":
		if rawCommand == "" {
			return Result{}, errArg("nagios: command is required when action=command")
		}
	case "servicegroup_host_downtime", "servicegroup_service_downtime":
		if host == "" || servicegroup == "" {
			return Result{}, errArg("nagios: action=%s requires host and servicegroup", action)
		}
	case "silence_nagios", "unsilence_nagios":
		// no required args
	default:
		return Result{}, errArg("nagios: unknown action %q", action)
	}

	cmdfile := argString(args, "cmdfile", "")
	if cmdfile == "" {
		cmdfile, err = nagiosWhichCmdfile(ctx, conn)
		if err != nil {
			return Result{}, err
		}
		if cmdfile == "" {
			return Fail("nagios: unable to locate nagios.cfg"), nil
		}
	}

	w := &nagiosWriter{ctx: ctx, conn: conn, cmdfile: cmdfile}

	switch action {
	case "downtime":
		switch svcMode {
		case "host":
			w.write(nagiosFmtDowntime("SCHEDULE_HOST_DOWNTIME", host, minutes, author, comment, start, ""))
		case "all":
			w.write(nagiosFmtDowntime("SCHEDULE_HOST_SVC_DOWNTIME", host, minutes, author, comment, start, ""))
		default:
			for _, svc := range servicesList {
				w.write(nagiosFmtDowntime("SCHEDULE_SVC_DOWNTIME", host, minutes, author, comment, start, svc))
			}
		}

	case "acknowledge":
		if svcMode == "host" {
			w.write(nagiosFmtAck("ACKNOWLEDGE_HOST_PROBLEM", host, author, comment, "", 0, 1, 0))
		} else {
			for _, svc := range servicesList {
				w.write(nagiosFmtAck("ACKNOWLEDGE_SVC_PROBLEM", host, author, comment, svc, 0, 1, 0))
			}
		}

	case "delete_downtime":
		switch svcMode {
		case "host":
			w.write(nagiosFmtDowntimeDelete("DEL_DOWNTIME_BY_HOST_NAME", host, "", nil, comment))
		case "all":
			w.write(nagiosFmtDowntimeDelete("DEL_DOWNTIME_BY_HOST_NAME", host, "", nil, ""))
		default:
			for _, svc := range servicesList {
				w.write(nagiosFmtDowntimeDelete("DEL_DOWNTIME_BY_HOST_NAME", host, svc, nil, comment))
			}
		}

	case "forced_check":
		switch svcMode {
		case "host":
			w.write(nagiosFmtCheck("SCHEDULE_FORCED_HOST_CHECK", host, ""))
		case "all":
			w.write(nagiosFmtCheck("SCHEDULE_FORCED_HOST_SVC_CHECKS", host, ""))
		default:
			for _, svc := range servicesList {
				w.write(nagiosFmtCheck("SCHEDULE_FORCED_SVC_CHECK", host, svc))
			}
		}

	case "servicegroup_host_downtime":
		w.write(nagiosFmtDowntime("SCHEDULE_SERVICEGROUP_HOST_DOWNTIME", servicegroup, minutes, author, comment, start, ""))

	case "servicegroup_service_downtime":
		w.write(nagiosFmtDowntime("SCHEDULE_SERVICEGROUP_SVC_DOWNTIME", servicegroup, minutes, author, comment, start, ""))

	case "silence":
		w.write(nagiosFmtNotif("DISABLE_HOST_SVC_NOTIFICATIONS", host, ""))
		w.write(nagiosFmtNotif("DISABLE_HOST_NOTIFICATIONS", host, ""))

	case "unsilence":
		w.write(nagiosFmtNotif("ENABLE_HOST_SVC_NOTIFICATIONS", host, ""))
		w.write(nagiosFmtNotif("ENABLE_HOST_NOTIFICATIONS", host, ""))

	case "enable_alerts":
		switch svcMode {
		case "host":
			w.write(nagiosFmtNotif("ENABLE_HOST_NOTIFICATIONS", host, ""))
		case "all":
			w.write(nagiosFmtNotif("ENABLE_HOST_SVC_NOTIFICATIONS", host, ""))
		default:
			for _, svc := range servicesList {
				w.write(nagiosFmtNotif("ENABLE_SVC_NOTIFICATIONS", host, svc))
			}
		}

	case "disable_alerts":
		switch svcMode {
		case "host":
			w.write(nagiosFmtNotif("DISABLE_HOST_NOTIFICATIONS", host, ""))
		case "all":
			w.write(nagiosFmtNotif("DISABLE_HOST_SVC_NOTIFICATIONS", host, ""))
		default:
			for _, svc := range servicesList {
				w.write(nagiosFmtNotif("DISABLE_SVC_NOTIFICATIONS", host, svc))
			}
		}

	case "silence_nagios":
		w.write(nagiosFmtNotif("DISABLE_NOTIFICATIONS", "", ""))

	case "unsilence_nagios":
		w.write(nagiosFmtNotif("ENABLE_NOTIFICATIONS", "", ""))

	case "command":
		w.write(fmt.Sprintf("[%d] %s\n", nagiosNow(), rawCommand))
	}

	if w.err != nil {
		return Result{}, w.err
	}
	if w.failMsg != "" {
		return Fail("nagios: " + w.failMsg), nil
	}
	res := Changed("")
	return res.WithExtra("nagios_commands", w.commands), nil
}

// nagiosWriter accumulates the write of one or more command-file lines,
// stopping at the first failure — matching this port's general
// fail-fast convention for a sequence of dependent shell operations.
type nagiosWriter struct {
	ctx      context.Context
	conn     remoteexec.Connection
	cmdfile  string
	commands []string
	failMsg  string
	err      error
}

func (w *nagiosWriter) write(cmd string) {
	if w.err != nil || w.failMsg != "" {
		return
	}
	failMsg, err := nagiosWriteCommand(w.ctx, w.conn, w.cmdfile, cmd)
	if err != nil {
		w.err = err
		return
	}
	if failMsg != "" {
		w.failMsg = failMsg
		return
	}
	w.commands = append(w.commands, strings.TrimRight(cmd, "\n"))
}

// nagiosWhichCmdfile probes nagiosCfgLocations in order for a
// `command_file = ...` line, matching real nagios's own which_cmdfile().
func nagiosWhichCmdfile(ctx context.Context, conn remoteexec.Connection) (string, error) {
	for _, path := range nagiosCfgLocations {
		exists, err := pathExists(ctx, conn, path)
		if err != nil {
			return "", err
		}
		if !exists {
			continue
		}
		out, err := run(ctx, conn, "cat "+shellQuote(path))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "command_file") {
				_, v, ok := strings.Cut(line, "=")
				if ok {
					return strings.TrimSpace(v), nil
				}
			}
		}
	}
	return "", nil
}

// nagiosWriteCommand writes cmd to the target's command-file FIFO,
// matching real nagios's own _write_command: the file must already
// exist and be a FIFO (checked with `test -p`, the POSIX equivalent of
// real nagios's own stat.S_ISFIFO check) — this port never creates it.
func nagiosWriteCommand(ctx context.Context, conn remoteexec.Connection, cmdfile, cmd string) (failMsg string, err error) {
	exists, err := pathExists(ctx, conn, cmdfile)
	if err != nil {
		return "", err
	}
	if !exists {
		return "nagios command file does not exist: " + cmdfile, nil
	}
	fifoRes, err := runStatus(ctx, conn, "test -p "+shellQuote(cmdfile))
	if err != nil {
		return "", err
	}
	if fifoRes.RC != 0 {
		return "nagios command file is not a fifo file: " + cmdfile, nil
	}
	writeRes, err := conn.Exec(ctx, "cat > "+shellQuote(cmdfile), strings.NewReader(cmd))
	if err != nil {
		return "", err
	}
	if writeRes.RC != 0 {
		return "unable to write to nagios command file: " + cmdfile, nil
	}
	return "", nil
}

func nagiosNow() int64 { return time.Now().Unix() }

// nagiosFmtDowntime formats a SCHEDULE_*_DOWNTIME external-command line,
// matching real nagios's own _fmt_dt_str. svc=="" means a host (not
// service) downtime; fixed=1/trigger=0 always, matching every one of
// real nagios's own call sites (none pass anything else).
func nagiosFmtDowntime(cmd, host string, minutes int, author, comment string, start *int64, svc string) string {
	entry := nagiosNow()
	st := entry
	if start != nil {
		st = *start
	}
	durationS := int64(minutes * 60)
	end := st + durationS
	hdr := fmt.Sprintf("[%d] %s;%s;", entry, cmd, host)
	var a []string
	if svc != "" {
		a = []string{svc, strconv.FormatInt(st, 10), strconv.FormatInt(end, 10), "1", "0", strconv.FormatInt(durationS, 10), author, comment}
	} else {
		a = []string{strconv.FormatInt(st, 10), strconv.FormatInt(end, 10), "1", "0", strconv.FormatInt(durationS, 10), author, comment}
	}
	return hdr + strings.Join(a, ";") + "\n"
}

// nagiosFmtAck formats an ACKNOWLEDGE_*_PROBLEM external-command line,
// matching real nagios's own _fmt_ack_str. sticky/notify/persistent are
// always 0/1/0, matching every one of real nagios's own call sites.
func nagiosFmtAck(cmd, host, author, comment, svc string, sticky, notify, persistent int) string {
	entry := nagiosNow()
	hdr := fmt.Sprintf("[%d] %s;%s;", entry, cmd, host)
	var a []string
	if svc != "" {
		a = []string{svc, strconv.Itoa(sticky), strconv.Itoa(notify), strconv.Itoa(persistent), author, comment}
	} else {
		a = []string{strconv.Itoa(sticky), strconv.Itoa(notify), strconv.Itoa(persistent), author, comment}
	}
	return hdr + strings.Join(a, ";") + "\n"
}

// nagiosFmtDowntimeDelete formats a DEL_DOWNTIME_BY_HOST_NAME
// external-command line, matching real nagios's own _fmt_dt_del_str —
// note (unlike the other formatters) every field is always present,
// empty ones included, since Nagios's own delete syntax is positional.
func nagiosFmtDowntimeDelete(cmd, host, svc string, start *int64, comment string) string {
	entry := nagiosNow()
	hdr := fmt.Sprintf("[%d] %s;%s;", entry, cmd, host)
	startStr := ""
	if start != nil {
		startStr = strconv.FormatInt(*start, 10)
	}
	a := []string{svc, startStr, comment}
	return hdr + strings.Join(a, ";") + "\n"
}

// nagiosFmtCheck formats a SCHEDULE_FORCED_*_CHECK[S] external-command
// line, matching real nagios's own _fmt_chk_str (check_time defaults to
// 3 seconds from now, matching real nagios's own `entry_time + 3`).
func nagiosFmtCheck(cmd, host, svc string) string {
	entry := nagiosNow()
	hdr := fmt.Sprintf("[%d] %s;%s;", entry, cmd, host)
	checkTime := strconv.FormatInt(entry+3, 10)
	if svc == "" {
		return hdr + checkTime + "\n"
	}
	return hdr + svc + ";" + checkTime + "\n"
}

// nagiosFmtNotif formats a notification-toggle external-command line
// (DISABLE_*/ENABLE_*_NOTIFICATIONS), matching real nagios's own
// _fmt_notif_str: host and svc are both optional trailing fields, only
// appended (`;`-prefixed) when non-empty — unlike nagiosFmtDowntimeDelete
// above, there is no positional padding here.
func nagiosFmtNotif(cmd, host, svc string) string {
	entry := nagiosNow()
	s := fmt.Sprintf("[%d] %s", entry, cmd)
	if host != "" {
		s += ";" + host
		if svc != "" {
			s += ";" + svc
		}
	}
	return s + "\n"
}
