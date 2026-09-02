package modules

import (
	"context"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleExpect implements (a best-effort approximation of) Ansible's
// `expect` module: runs a command and responds to interactive prompts.
//
// Real ansible.builtin.expect is a limited pexpect: it launches the
// command and, as each prompt (matched by a Python regex) appears on
// its output stream, writes the corresponding answer back to its
// stdin — a genuinely interactive, conditional exchange. This port's
// Connection.Exec is a single blocking round trip (cmd in, one
// captured Result out) with no way to observe output as it arrives or
// write to stdin mid-command, so true prompt-matching is not
// implementable against this abstraction as currently designed.
//
// What this port does instead: run the command via conn.Exec, and if
// `responses` is given, concatenate its values (in map-key sorted
// order, for determinism — real expect answers prompts in the order
// they're seen on the command's actual output, which this port cannot
// observe) into stdin, one per line, joined with newlines. This is a
// "feed some stdin and hope the command's prompts consume it in the
// same order" approximation, NOT real expect/prompt-matching behavior:
// the `responses` keys (each a regex matched against a specific
// prompt) are not matched against anything here, and a command whose
// prompts arrive in a different order, or that reads from a tty
// instead of stdin, will not behave as it would under real expect.
//
// Args: command (string, required); responses (map[string]any,
// optional) — each value a string or list of strings; only the first
// element of a list value is used (a real conditional
// "different answer for the Nth occurrence" is exactly the interactive
// behavior this port can't provide); chdir (string, optional); creates,
// removes (string, optional, same short-circuit as command/shell).
func moduleExpect(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	command, err := requireString(args, "command")
	if err != nil {
		return Result{}, err
	}

	if skip, msg, err := skipByCreatesRemoves(ctx, conn, args); err != nil {
		return Result{}, err
	} else if skip {
		return Ok(msg), nil
	}

	cmdLine := command
	if chdir := argString(args, "chdir", ""); chdir != "" {
		cmdLine = "cd " + shellQuote(chdir) + " && " + cmdLine
	}

	stdin := expectStdin(args)

	var res remoteexec.Result
	if stdin != "" {
		res, err = conn.Exec(ctx, cmdLine, strings.NewReader(stdin))
	} else {
		res, err = conn.Exec(ctx, cmdLine, nil)
	}
	if err != nil {
		return Result{}, err
	}
	return commandResult([]string{command}, res), nil
}

// expectStdin flattens the `responses` argument into newline-joined
// stdin, in sorted-key order (see moduleExpect's doc comment for why
// this is only an approximation of real expect's prompt matching).
func expectStdin(args map[string]any) string {
	v, ok := args["responses"]
	if !ok {
		return ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		switch val := m[k].(type) {
		case string:
			lines = append(lines, val)
		case []any:
			if len(val) > 0 {
				lines = append(lines, argToString(val[0]))
			}
		case []string:
			if len(val) > 0 {
				lines = append(lines, val[0])
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func argToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
