package modules

import (
	"context"
	"path/filepath"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleScript implements (a subset of) Ansible's `script` module:
// uploads a local script to the target and runs it there.
//
// Args: cmd (or `free_form`/`_raw_params`, matching how real script's
// free-form parameter is normally passed) — the LOCAL script's path,
// followed by optional space-delimited arguments to pass it, required;
// chdir (string, optional) — a directory on the TARGET to run the
// script from; creates, removes (string, optional, TARGET paths, same
// short-circuit as command/shell); executable (string, optional) — an
// interpreter to invoke the uploaded script with (e.g.
// "/usr/bin/python3"); when unset, the script is executed directly
// (relying on its own shebang line and the +x bit conn.Put sets via
// PutOptions.Executable).
//
// Splitting cmd into "local path" plus "trailing args" is done with a
// plain whitespace split (strings.Fields) — unlike real script (and
// unlike moduleCommand's tokenize, which understands quoting), a local
// path containing a space is not supported here.
//
// The uploaded copy is removed from the target after running,
// best-effort: a failure to remove it does not fail the task (the
// script's own exit status is what matters; leaving a stray temp file
// behind on a cleanup error is judged the lesser problem versus
// reporting an otherwise-successful script run as failed).
func moduleScript(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	cmdStr := argString(args, "cmd", argString(args, "free_form", argString(args, "_raw_params", "")))
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return Result{}, errArg("script: missing required argument: cmd")
	}
	fields := strings.Fields(cmdStr)
	localPath := fields[0]
	scriptArgs := strings.Join(fields[1:], " ")

	if skip, msg, err := skipByCreatesRemoves(ctx, conn, args); err != nil {
		return Result{}, err
	} else if skip {
		return Ok(msg), nil
	}

	remotePath := conn.TempPath(filepath.Base(localPath))
	if err := conn.Put(ctx, localPath, remotePath, remoteexec.PutOptions{Executable: true}); err != nil {
		return Result{}, err
	}

	execCmd := shellQuote(remotePath)
	if executable := argString(args, "executable", ""); executable != "" {
		execCmd = shellQuote(executable) + " " + execCmd
	}
	if scriptArgs != "" {
		execCmd += " " + scriptArgs
	}
	if chdir := argString(args, "chdir", ""); chdir != "" {
		execCmd = "cd " + shellQuote(chdir) + " && " + execCmd
	}

	res, err := conn.Exec(ctx, execCmd, nil)
	if err != nil {
		return Result{}, err
	}
	_ = conn.Remove(ctx, remotePath) // best-effort cleanup; see doc comment

	return commandResult([]string{localPath}, res), nil
}
