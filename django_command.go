package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDjangoCommand implements (a subset of) Ansible's
// `django_command` module: runs a single Django admin command via
// `python -m django`, matching the real module's own description of
// what `command` must be ("a valid command accepted by `python -m
// django` at the target system").
//
// Args: command (string, required) — a single django-admin subcommand
// name (e.g. "check", "migrate"); real django_command puts any extra
// flags for it in extra_args rather than folding them into command
// (see the module's own examples, which only ever pass a bare
// subcommand name), so this port does the same and treats command as
// one opaque token rather than tokenizing it — contrast with
// django_manage.go's `command`, which real Ansible's own examples
// embed inline flags into directly. extra_args ([]string, optional) —
// appended verbatim as extra argv tokens. pythonpath (string,
// optional) — passed as `--pythonpath`. settings (string, optional) —
// passed as `--settings`. skip_checks (bool, default false) — passed
// as `--skip-checks`. traceback (bool, default false) — passed as
// `--traceback`. verbosity (int, optional, one of 0-3) — passed as
// `--verbosity`. venv (string, optional) — the python interpreter is
// taken from "<venv>/bin/python" instead of plain "python" when set.
//
// Real django-admin is always run under the `C` locale with
// `--no-color`, per the module's own documented NOTE; this port
// reproduces that exactly.
//
// Simplifications vs real django_command:
//
//   - No `run_info` or `version` return values. Both require parsing
//     django-admin's own stdout, which this port does not attempt.
//   - This is an action module, like command.go's own `command`/
//     `shell`: a zero exit is unconditionally reported as Changed,
//     since "did this django-admin command actually change anything"
//     is exactly as opaque to a shell probe as "did apt-get install
//     actually change anything" already is for apt.go's own
//     `state: latest` branch — see that file's comment for the same
//     tradeoff, applied here to every django-admin invocation.
//   - Real django_command has no `project_path`/`chdir` argument at
//     all (unlike django_manage — see django_manage.go), and this port
//     doesn't add one either: django-admin is expected to locate the
//     project via `pythonpath`/`settings` alone.
func moduleDjangoCommand(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	command, err := requireString(args, "command")
	if err != nil {
		return Result{}, err
	}
	extraArgs := argStringList(args, "extra_args")
	pythonpath := argString(args, "pythonpath", "")
	settings := argString(args, "settings", "")
	skipChecks := argBool(args, "skip_checks", false)
	traceback := argBool(args, "traceback", false)
	venv := argString(args, "venv", "")
	verbosity := argInt(args, "verbosity", -1)
	if verbosity != -1 && (verbosity < 0 || verbosity > 3) {
		return Result{}, errArg("django_command: verbosity must be 0-3, got %d", verbosity)
	}

	python := "python"
	if venv != "" {
		python = strings.TrimRight(venv, "/") + "/bin/python"
	}

	argv := []string{python, "-m", "django", command, "--no-color"}
	if pythonpath != "" {
		argv = append(argv, "--pythonpath", pythonpath)
	}
	if settings != "" {
		argv = append(argv, "--settings", settings)
	}
	if skipChecks {
		argv = append(argv, "--skip-checks")
	}
	if traceback {
		argv = append(argv, "--traceback")
	}
	if verbosity != -1 {
		argv = append(argv, "--verbosity", strconv.Itoa(verbosity))
	}
	argv = append(argv, extraArgs...)

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmdLine := "env LC_ALL=C " + strings.Join(quoted, " ")

	res, err := conn.Exec(ctx, cmdLine, nil)
	if err != nil {
		return Result{}, err
	}
	return commandResult(argv, res), nil
}
