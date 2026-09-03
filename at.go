package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAt implements (a subset of) Ansible's `at` module: schedules a
// one-shot command (or an existing script file) to run once in the
// future via the `at` command.
//
// Args: command (string) or script_file (string, a path already on the
// target — this port does not upload a local script_file the way
// script.go does for `script`; real at's own `script_file` argument
// documents it as "an existing script file", i.e. also target-side, so
// this matches) — exactly one is required; count (int) and units
// (minutes|hours|days|weeks) — together the "now + count units" time
// spec; both are required when state=present, since this port
// implements no other way to express a future time (real at has no
// absolute-time argument either — count+units is the whole mechanism);
// state (present|absent, default "present"); unique (bool, default
// false) — for state=present with `command`, skip scheduling if a
// pending job already matches this exact command text.
//
// Matching an existing job (for `unique` and for state=absent) is done
// by dumping every pending job's script via `at -c <jobnum>` and
// grep -F'ing for the command text — there is no unique-ID marker
// mechanism here (real Ansible's own at module tags scheduled jobs the
// same way, by grepping job content for the command). This only works
// for command-based jobs: state=absent and unique=true are NOT
// supported for script_file (this port requires `command` for
// matching) — a real gap versus real at, which can match either form;
// documented rather than silently ignored.
func moduleAt(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	command := argString(args, "command", "")
	scriptFile := argString(args, "script_file", "")
	if command == "" && scriptFile == "" {
		return Result{}, errArg("at: one of command or script_file is required")
	}
	if command != "" && scriptFile != "" {
		return Result{}, errArg("at: command and script_file are mutually exclusive")
	}
	state := argString(args, "state", "present")

	switch state {
	case "absent":
		if command == "" {
			return Result{}, errArg("at: state=absent requires command (matching by script_file is not supported by this port)")
		}
		jobs, err := atFindJobs(ctx, conn, command)
		if err != nil {
			return Result{}, err
		}
		if len(jobs) == 0 {
			return Ok("no matching job"), nil
		}
		if _, err := run(ctx, conn, "atrm "+strings.Join(jobs, " ")); err != nil {
			return Result{}, err
		}
		return Changed("removed job(s) " + strings.Join(jobs, ", ")), nil

	case "present":
		_, ok := args["count"]
		units := argString(args, "units", "")
		if !ok || units == "" {
			return Result{}, errArg("at: count and units are both required when state=present")
		}
		n := argInt(args, "count", 0)
		timeSpec := "now + " + strconv.Itoa(n) + " " + units

		unique := argBool(args, "unique", false)
		if unique && command != "" {
			jobs, err := atFindJobs(ctx, conn, command)
			if err != nil {
				return Result{}, err
			}
			if len(jobs) > 0 {
				return Ok("a matching job is already scheduled"), nil
			}
		}

		var cmd string
		if scriptFile != "" {
			cmd = "at -f " + shellQuote(scriptFile) + " " + timeSpec
			if _, err := run(ctx, conn, cmd); err != nil {
				return Result{}, err
			}
		} else {
			cmd = "at " + timeSpec
			res, err := conn.Exec(ctx, cmd, strings.NewReader(command+"\n"))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("at: " + strings.TrimSpace(res.Stderr)), nil
			}
		}
		return Changed("job scheduled"), nil

	default:
		return Result{}, errArg("at: state must be present or absent, got %q", state)
	}
}

// atFindJobs returns the job numbers (from `atq`) whose script content
// (`at -c <jobnum>`) contains matchText.
func atFindJobs(ctx context.Context, conn remoteexec.Connection, matchText string) ([]string, error) {
	script := `for j in $(atq 2>/dev/null | awk '{print $1}'); do ` +
		`if at -c "$j" 2>/dev/null | grep -qF -- ` + shellQuote(matchText) + `; then echo "$j"; fi; done`
	out, err := run(ctx, conn, script)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}
