package modules

import (
	"context"
	"fmt"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCommand implements Ansible's `command` module: runs a program
// with an argument list, never interpreting shell metacharacters in
// those arguments (pipes, redirects, `;` are passed through as literal
// argv entries, not executed) — for shell features, use `shell`.
//
// Args: cmd (string) or argv (list) — the command; chdir; creates;
// removes.
func moduleCommand(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	argv, err := commandArgv(args)
	if err != nil {
		return Result{}, err
	}

	cmdLine, skip, skipMsg, err := ComposeCommandLine(ctx, conn, "command", args)
	if err != nil {
		return Result{}, err
	}
	if skip {
		return Ok(skipMsg), nil
	}

	start := time.Now()
	res, err := conn.Exec(ctx, cmdLine, nil)
	end := time.Now()
	if err != nil {
		return Result{}, err
	}
	return commandResult(argv, res).withTiming(start, end), nil
}

// ComposeCommandLine composes the exact shell command line the
// "command" or "shell" module would execute for args (argv-quoting/
// chdir handling included), running the real creates/removes
// short-circuit check against conn but WITHOUT executing the command
// itself — skip=true means the command should not run at all (skipMsg
// explains why, matching what a synchronous run would return via
// Ok(msg) instead of actually running anything).
//
// Exported for go-ansible/playbook's async: task launcher: command and
// shell are the only two modules whose entire job reduces to one
// remote command, and so the only two this port can genuinely
// background on the target the way async requires — the launcher needs
// the exact command a synchronous run would use, to wrap it instead of
// running it directly.
func ComposeCommandLine(ctx context.Context, conn remoteexec.Connection, module string, args map[string]any) (cmdLine string, skip bool, skipMsg string, err error) {
	switch module {
	case "command":
		argv, err := commandArgv(args)
		if err != nil {
			return "", false, "", err
		}
		if skip, msg, err := skipByCreatesRemoves(ctx, conn, args); err != nil {
			return "", false, "", err
		} else if skip {
			return "", true, msg, nil
		}
		quoted := make([]string, len(argv))
		for i, a := range argv {
			quoted[i] = shellQuote(a)
		}
		cmdLine := strings.Join(quoted, " ")
		if chdir := argString(args, "chdir", ""); chdir != "" {
			cmdLine = "cd " + shellQuote(chdir) + " && " + cmdLine
		}
		return cmdLine, false, "", nil
	case "shell":
		cmdStr := argString(args, "cmd", argString(args, "_raw_params", ""))
		if strings.TrimSpace(cmdStr) == "" {
			return "", false, "", errArg("shell: missing required argument: cmd")
		}
		if skip, msg, err := skipByCreatesRemoves(ctx, conn, args); err != nil {
			return "", false, "", err
		} else if skip {
			return "", true, msg, nil
		}
		full := cmdStr
		if chdir := argString(args, "chdir", ""); chdir != "" {
			full = "cd " + shellQuote(chdir) + " && " + cmdStr
		}
		return full, false, "", nil
	default:
		return "", false, "", fmt.Errorf("ComposeCommandLine: unsupported module %q (only command/shell)", module)
	}
}

// moduleShell implements Ansible's `shell` module: runs cmd through the
// target's real shell, so pipes/redirects/globs/`;` behave as they
// would typed at a prompt.
//
// Args: cmd (string) — the command line; chdir; creates; removes.
func moduleShell(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cmdStr := argString(args, "cmd", argString(args, "_raw_params", ""))
	if strings.TrimSpace(cmdStr) == "" {
		return Result{}, errArg("shell: missing required argument: cmd")
	}

	full, skip, skipMsg, err := ComposeCommandLine(ctx, conn, "shell", args)
	if err != nil {
		return Result{}, err
	}
	if skip {
		return Ok(skipMsg), nil
	}

	start := time.Now()
	res, err := conn.Exec(ctx, full, nil)
	end := time.Now()
	if err != nil {
		return Result{}, err
	}
	// shell reports cmd as the command STRING, where command reports the
	// argv list — measured against real ansible-core: `shell: echo four`
	// gives "echo four", `command: echo one two` gives
	// ["echo", "one", "two"]. This port wrapped the shell string in a
	// one-element list.
	return commandResult(nil, res).WithExtra("cmd", cmdStr).withTiming(start, end), nil
}

func commandArgv(args map[string]any) ([]string, error) {
	if list := argStringList(args, "argv"); list != nil {
		return list, nil
	}
	cmdStr := argString(args, "cmd", argString(args, "_raw_params", ""))
	if strings.TrimSpace(cmdStr) == "" {
		return nil, errArg("command: missing required argument: cmd or argv")
	}
	return tokenize(cmdStr), nil
}

func commandResult(argv []string, res remoteexec.Result) Result {
	r := Result{Changed: true, Failed: res.RC != 0}
	if r.Failed {
		// Real Ansible's own wording. The code itself is in rc, which is
		// why the message does not repeat it.
		r.Msg = "The command exited with a non-zero return code."
	}
	if argv != nil {
		r = r.WithExtra("cmd", argv)
	}
	r = r.WithExtra("stdout", res.Stdout)
	r = r.WithExtra("stderr", res.Stderr)
	r = r.WithExtra("rc", res.RC)
	return r
}

// skipByCreatesRemoves implements the `creates`/`removes` short-circuit
// shared by command/shell: skip (unchanged) if `creates` already exists,
// or if `removes` does not exist.
func skipByCreatesRemoves(ctx context.Context, conn remoteexec.Connection, args map[string]any) (skip bool, msg string, err error) {
	if creates := argString(args, "creates", ""); creates != "" {
		exists, err := pathExists(ctx, conn, creates)
		if err != nil {
			return false, "", err
		}
		if exists {
			return true, fmt.Sprintf("skipped, since %s exists", creates), nil
		}
	}
	if removes := argString(args, "removes", ""); removes != "" {
		exists, err := pathExists(ctx, conn, removes)
		if err != nil {
			return false, "", err
		}
		if !exists {
			return true, fmt.Sprintf("skipped, since %s does not exist", removes), nil
		}
	}
	return false, "", nil
}

// tokenize splits a command line into words, honoring single/double
// POSIX quoting and backslash escaping outside quotes.
func tokenize(s string) []string {
	var toks []string
	var cur strings.Builder
	inWord := false
	i := 0
	for i < len(s) {
		ch := s[i]
		switch {
		case ch == ' ' || ch == '\t':
			if inWord {
				toks = append(toks, cur.String())
				cur.Reset()
				inWord = false
			}
			i++
		case ch == '\'':
			inWord = true
			i++
			for i < len(s) && s[i] != '\'' {
				cur.WriteByte(s[i])
				i++
			}
			i++
		case ch == '"':
			inWord = true
			i++
			for i < len(s) && s[i] != '"' {
				cur.WriteByte(s[i])
				i++
			}
			i++
		case ch == '\\' && i+1 < len(s):
			inWord = true
			cur.WriteByte(s[i+1])
			i += 2
		default:
			inWord = true
			cur.WriteByte(ch)
			i++
		}
	}
	if inWord {
		toks = append(toks, cur.String())
	}
	return toks
}

// withTiming adds the start/end/delta fields real Ansible's command and
// shell modules report, in real Ansible's own spellings: a timestamp is
// "2006-01-02 15:04:05.000000" and delta is Python's str(timedelta).
// A playbook that reads result.delta to time a step gets nothing without
// them.
func (r Result) withTiming(start, end time.Time) Result {
	const stamp = "2006-01-02 15:04:05.000000"
	return r.WithExtra("start", start.Format(stamp)).
		WithExtra("end", end.Format(stamp)).
		WithExtra("delta", formatDelta(end.Sub(start)))
}

// formatDelta renders d the way Python's str(timedelta) does — the exact
// shape real Ansible puts in result.delta. Hours are not zero-padded,
// minutes and seconds are, and the fractional part is six digits and is
// omitted entirely when it is zero.
func formatDelta(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d / time.Hour)
	m := int(d%time.Hour) / int(time.Minute)
	sec := int(d%time.Minute) / int(time.Second)
	micro := int(d%time.Second) / int(time.Microsecond)
	if micro == 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d:%02d.%06d", h, m, sec, micro)
}
