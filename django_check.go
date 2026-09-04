package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDjangoCheck implements Ansible's `django_check` module: runs
// `django-admin check` (invoked as `python -m django check`, matching
// django_command.go/django_manage.go's own convention for how this
// port reaches django-admin) to validate a Django project without
// touching the database (unless databases is given).
//
// Args: settings (string, required) — passed as `--settings`. Real
// django_check/_createcachetable/_dumpdata/_loaddata all share the
// same `_django` argument fragment (see module_utils/_django.py's own
// `django_std_args`), in which settings IS required — unlike
// django_command.go's own docstring for the sibling django_command
// module, which documents settings as optional; that is itself a
// pre-existing deviation from real Ansible already shipped in this
// port and is not repeated here. databases ([]string, optional; alias
// database) — one `--database` flag per value. deploy (bool, default
// false) — `--deploy`. fail_level (string, optional, one of CRITICAL|
// ERROR|WARNING|INFO|DEBUG) — `--fail-level`. tags ([]string,
// optional) — one `--tag` flag per value. apps ([]string, optional) —
// appended as positional arguments. pythonpath, skip_checks,
// traceback, venv, verbosity: shared with django_command.go/
// django_manage.go, same meaning (see djangoAdminArgv below, this
// file's shared helper, reused by django_createcachetable.go/
// django_dumpdata.go/django_loaddata.go rather than each duplicating
// the common `python -m django <cmd> --no-color --settings ...`
// composition).
//
// Real django_check always runs `<python> -m django --version` first
// and returns it as Extra["version"]; this port does the same (see
// djangoAdminRun). Real django_check's `run_info` return value
// (populated only at verbosity>=3, command-runner debug metadata) is
// not reproduced — matching django_command.go's own documented
// simplification for the same omission.
func moduleDjangoCheck(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	settings, err := requireString(args, "settings")
	if err != nil {
		return Result{}, err
	}
	dbs := argStringList(args, "databases")
	if len(dbs) == 0 {
		dbs = argStringList(args, "database")
	}
	deploy := argBool(args, "deploy", false)
	failLevel := argString(args, "fail_level", "")
	switch failLevel {
	case "", "CRITICAL", "ERROR", "WARNING", "INFO", "DEBUG":
	default:
		return Result{}, errArg("django_check: fail_level must be one of CRITICAL, ERROR, WARNING, INFO, DEBUG, got %q", failLevel)
	}
	tags := argStringList(args, "tags")
	apps := argStringList(args, "apps")

	var extra []string
	for _, db := range dbs {
		extra = append(extra, "--database", db)
	}
	if deploy {
		extra = append(extra, "--deploy")
	}
	if failLevel != "" {
		extra = append(extra, "--fail-level", failLevel)
	}
	for _, tag := range tags {
		extra = append(extra, "--tag", tag)
	}
	extra = append(extra, apps...)

	return djangoAdminRun(ctx, conn, args, "check", settings, extra)
}

// djangoAdminArgv builds the shared argv prefix
// [<python> -m django <command> --no-color --settings <settings> ...]
// through --skip-checks — the flags common to django_check/
// _createcachetable/_dumpdata/_loaddata, matching django_command.go's
// own composition for the same flags (this file's shared helper exists
// because django_command.go/django_manage.go cannot be modified to
// export it — see this batch's task instructions — so it is
// re-established here rather than duplicated four separate times
// across this file's own siblings).
func djangoAdminArgv(args map[string]any, command, settings string) (python string, argv []string, err error) {
	venv := argString(args, "venv", "")
	python = "python"
	if venv != "" {
		python = strings.TrimRight(venv, "/") + "/bin/python"
	}
	pythonpath := argString(args, "pythonpath", "")
	skipChecks := argBool(args, "skip_checks", false)
	traceback := argBool(args, "traceback", false)
	verbosity := argInt(args, "verbosity", -1)
	if verbosity != -1 && (verbosity < 0 || verbosity > 3) {
		return "", nil, errArg("django_%s: verbosity must be 0-3, got %d", command, verbosity)
	}

	argv = []string{python, "-m", "django", command, "--no-color", "--settings", settings}
	if pythonpath != "" {
		argv = append(argv, "--pythonpath", pythonpath)
	}
	if traceback {
		argv = append(argv, "--traceback")
	}
	if verbosity != -1 {
		argv = append(argv, "--verbosity", strconv.Itoa(verbosity))
	}
	if skipChecks {
		argv = append(argv, "--skip-checks")
	}
	return python, argv, nil
}

// djangoAdminRun runs `<python> -m django <command> ...` (base flags
// from djangoAdminArgv plus extra, the subcommand-specific flags) and
// returns a commandResult with Extra["version"] set from
// `<python> -m django --version`, matching real django_check/
// _createcachetable/_dumpdata/_loaddata's own DjangoModuleHelper.
// __run__, which always captures that version string as its own
// "version" return value before running the requested subcommand. A
// version-probe failure (django-admin missing or broken) is reported
// as Result{Failed:true}, not a Go error, matching real code's own
// fail_json-on-subprocess-failure behavior rather than crashing.
func djangoAdminRun(ctx context.Context, conn remoteexec.Connection, args map[string]any, command, settings string, extra []string) (Result, error) {
	python, base, err := djangoAdminArgv(args, command, settings)
	if err != nil {
		return Result{}, err
	}

	verRes, err := runStatus(ctx, conn, "env LC_ALL=C "+shellQuote(python)+" -m django --version")
	if err != nil {
		return Result{}, err
	}
	if verRes.RC != 0 {
		return Fail("django_" + command + ": could not determine Django version: " + strings.TrimSpace(verRes.Stderr)), nil
	}
	version := strings.TrimSpace(verRes.Stdout)

	argv := append(base, extra...)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmdLine := "env LC_ALL=C " + strings.Join(quoted, " ")

	res, err := conn.Exec(ctx, cmdLine, nil)
	if err != nil {
		return Result{}, err
	}
	return commandResult(argv, res).WithExtra("version", version), nil
}
