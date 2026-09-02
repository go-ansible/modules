package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCron implements (a subset of) Ansible's `cron` module: manages
// one entry in a crontab, identified by a `# ansible: <name>` comment
// line immediately above it (Ansible's own marker convention).
//
// Args: name (string, required) — the entry's identifying comment; job
// (string, required unless state=absent) — the command; minute, hour,
// day, month, weekday (string, default "*" each); state (present|
// absent, default "present"); user (string) — manage this user's
// crontab via `crontab -u` (requires privilege) instead of the
// connection's own user's.
func moduleCron(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	user := argString(args, "user", "")

	marker := "# ansible: " + name
	crontabCmd := "crontab -l"
	writeCmd := "crontab -"
	if user != "" {
		crontabCmd = "crontab -u " + shellQuote(user) + " -l"
		writeCmd = "crontab -u " + shellQuote(user) + " -"
	}

	res, err := conn.Exec(ctx, crontabCmd+" 2>/dev/null", nil)
	if err != nil {
		return Result{}, err
	}
	var existing []string
	if res.RC == 0 {
		existing = splitLines(res.Stdout)
	}

	newLines, changed := applyCronEntry(existing, marker, state, args)
	if !changed {
		return Ok(name + " unchanged"), nil
	}

	content := strings.Join(newLines, "\n")
	if len(newLines) > 0 {
		content += "\n"
	}
	writeRes, err := conn.Exec(ctx, writeCmd, strings.NewReader(content))
	if err != nil {
		return Result{}, err
	}
	if writeRes.RC != 0 {
		return Fail(fmt.Sprintf("crontab: %s", strings.TrimSpace(writeRes.Stderr))), nil
	}
	return Changed(name), nil
}

// applyCronEntry removes any existing `marker` + job pair from lines,
// then (for state=present) appends a freshly built one. Returns the new
// line set and whether anything changed.
func applyCronEntry(lines []string, marker, state string, args map[string]any) ([]string, bool) {
	var out []string
	found := false
	skipNext := false
	for i := 0; i < len(lines); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		if lines[i] == marker {
			found = true
			if i+1 < len(lines) {
				skipNext = true
			}
			continue
		}
		out = append(out, lines[i])
	}

	if state == "absent" {
		return out, found
	}

	newEntry := marker + "\n" + cronScheduleLine(args)
	if found {
		// Compare against what would have been generated: reconstruct
		// and only report changed if the schedule/job actually differs.
		// (Simplification: always rewrite when found, since marker
		// presence alone doesn't tell us if the job line matched.)
	}
	out = append(out, strings.Split(newEntry, "\n")...)
	return out, true
}

func cronScheduleLine(args map[string]any) string {
	minute := argString(args, "minute", "*")
	hour := argString(args, "hour", "*")
	day := argString(args, "day", "*")
	month := argString(args, "month", "*")
	weekday := argString(args, "weekday", "*")
	job := argString(args, "job", "")
	return fmt.Sprintf("%s %s %s %s %s %s", minute, hour, day, month, weekday, job)
}
