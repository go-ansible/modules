package modules

import (
	"context"
	"fmt"
	"strings"

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

	if skip, msg, err := skipByCreatesRemoves(ctx, conn, args); err != nil {
		return Result{}, err
	} else if skip {
		return Ok(msg), nil
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmdLine := strings.Join(quoted, " ")
	if chdir := argString(args, "chdir", ""); chdir != "" {
		cmdLine = "cd " + shellQuote(chdir) + " && " + cmdLine
	}

	res, err := conn.Exec(ctx, cmdLine, nil)
	if err != nil {
		return Result{}, err
	}
	return commandResult(argv, res), nil
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

	if skip, msg, err := skipByCreatesRemoves(ctx, conn, args); err != nil {
		return Result{}, err
	} else if skip {
		return Ok(msg), nil
	}

	full := cmdStr
	if chdir := argString(args, "chdir", ""); chdir != "" {
		full = "cd " + shellQuote(chdir) + " && " + cmdStr
	}

	res, err := conn.Exec(ctx, full, nil)
	if err != nil {
		return Result{}, err
	}
	return commandResult([]string{cmdStr}, res), nil
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
		r.Msg = fmt.Sprintf("non-zero return code: %d", res.RC)
	}
	r = r.WithExtra("cmd", argv)
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
