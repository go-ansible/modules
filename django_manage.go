package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDjangoManage implements (a subset of) Ansible's `django_manage`
// module: runs a Django management command through a project's own
// `manage.py`. This is a genuinely different real Ansible module from
// django_command (django_command.go) — not a near-duplicate — since
// django_manage always operates against a project directory and its
// manage.py, has dedicated parameters for several built-in subcommands
// (collectstatic/createcachetable/flush/loaddata/migrate/test), and
// allows arbitrary custom commands with their flags embedded directly
// in `command`, none of which django_command supports. This port keeps
// them as two separate implementations for that reason.
//
// Args:
//
//   - command (string, required) — the manage.py subcommand. Unlike
//     django_command's `command`, this one MAY embed its own inline
//     flags verbatim (e.g. "createsuperuser --noinput
//     --username=admin"), matching real django_manage's own examples.
//     This port tokenizes it with the same POSIX-quote-aware tokenize()
//     command.go already uses for `shell`, then re-quotes each token,
//     rather than splicing the raw string into the shell command line.
//   - project_path (string, required; aliases app_path, chdir) —
//     directory containing manage.py. The module cd's there and
//     invokes "./manage.py", relying — like real django_manage, per its
//     own documented NOTE — on manage.py's own shebang, unless
//     virtualenv is set.
//   - virtualenv (string, optional; alias virtual_env) — when set,
//     "<virtualenv>/bin/python manage.py" is invoked instead of
//     "./manage.py", and the module fails if <virtualenv> does not
//     exist on the target, matching real django_manage's documented
//     behavior that it does NOT create the virtualenv itself.
//   - settings (string, optional) — passed as `--settings`.
//   - pythonpath (string, optional; alias python_path) — passed as
//     `--pythonpath`.
//   - database (string, optional) — passed as `--database`, added only
//     when command is one of createcachetable/flush/loaddata/migrate,
//     matching the exact set real django_manage documents it for.
//   - cache_table (string, optional) — appended as a positional
//     argument, only when command is "createcachetable".
//   - clear (bool, default false) and link (bool, optional) —
//     `--clear`/`--link`, only when command is "collectstatic";
//     `--noinput` is always added for collectstatic, per real
//     django_manage's own documented behavior.
//   - merge (bool, optional) and skip (bool, optional) —
//     `--merge`/`--skip`, only when command is "migrate".
//   - apps (string, optional) — space-delimited app labels, appended as
//     positional arguments, only when command is "test".
//   - fixtures (string, optional) — space-delimited fixture names,
//     appended as positional arguments, only when command is
//     "loaddata".
//   - failfast (bool, default false; alias fail_fast) — `--failfast`,
//     only when command is "test".
//   - testrunner (string, optional; alias test_runner) — `--testrunner`,
//     only when command is "test".
//
// Simplifications vs real django_manage:
//
//   - No `createcachetable` "table already exists" output parsing.
//     Real django_manage greps the (English-only, per its own NOTE)
//     error message to report Changed=false when the table already
//     existed; this port always reports Changed on a zero exit — the
//     same "can't cheaply tell idempotency apart" tradeoff bundler.go
//     documents for `bundle install`/`update`, applied here to every
//     manage.py subcommand, not just createcachetable.
//   - The virtualenv existence check only confirms the directory
//     exists, not that it contains a working Python interpreter.
//   - No owner/group/selinux/attributes/directory_mode handling
//     (real django_manage has none of those either).
func moduleDjangoManage(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	command, err := requireString(args, "command")
	if err != nil {
		return Result{}, err
	}
	projectPath := djangoManageFirst(args, "project_path", "app_path", "chdir")
	if projectPath == "" {
		return Result{}, errArg("django_manage: missing required argument: project_path (or app_path/chdir)")
	}
	virtualenv := djangoManageFirst(args, "virtualenv", "virtual_env")
	settings := argString(args, "settings", "")
	pythonpath := djangoManageFirst(args, "pythonpath", "python_path")
	database := argString(args, "database", "")
	cacheTable := argString(args, "cache_table", "")
	clear := argBool(args, "clear", false)
	link := argBool(args, "link", false)
	merge := argBool(args, "merge", false)
	skip := argBool(args, "skip", false)
	apps := argString(args, "apps", "")
	fixtures := argString(args, "fixtures", "")
	failfast := argBool(args, "failfast", argBool(args, "fail_fast", false))
	testrunner := djangoManageFirst(args, "testrunner", "test_runner")

	if virtualenv != "" {
		exists, err := pathExists(ctx, conn, virtualenv)
		if err != nil {
			return Result{}, err
		}
		if !exists {
			return Fail("django_manage: virtualenv " + virtualenv + " does not exist"), nil
		}
	}

	toks := tokenize(command)
	if len(toks) == 0 {
		return Result{}, errArg("django_manage: command must not be empty")
	}
	cmdName := toks[0]

	var argv []string
	if virtualenv != "" {
		argv = append(argv, strings.TrimRight(virtualenv, "/")+"/bin/python", "manage.py")
	} else {
		argv = append(argv, "./manage.py")
	}
	argv = append(argv, toks...)

	switch cmdName {
	case "collectstatic":
		argv = append(argv, "--noinput")
		if clear {
			argv = append(argv, "--clear")
		}
		if link {
			argv = append(argv, "--link")
		}
	case "migrate":
		if merge {
			argv = append(argv, "--merge")
		}
		if skip {
			argv = append(argv, "--skip")
		}
	case "test":
		if apps != "" {
			argv = append(argv, strings.Fields(apps)...)
		}
		if failfast {
			argv = append(argv, "--failfast")
		}
		if testrunner != "" {
			argv = append(argv, "--testrunner", testrunner)
		}
	case "loaddata":
		if fixtures != "" {
			argv = append(argv, strings.Fields(fixtures)...)
		}
	case "createcachetable":
		if cacheTable != "" {
			argv = append(argv, cacheTable)
		}
	}
	switch cmdName {
	case "createcachetable", "flush", "loaddata", "migrate":
		if database != "" {
			argv = append(argv, "--database", database)
		}
	}
	if settings != "" {
		argv = append(argv, "--settings", settings)
	}
	if pythonpath != "" {
		argv = append(argv, "--pythonpath", pythonpath)
	}

	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmdLine := "cd " + shellQuote(projectPath) + " && " + strings.Join(quoted, " ")

	res, err := conn.Exec(ctx, cmdLine, nil)
	if err != nil {
		return Result{}, err
	}
	return commandResult(argv, res), nil
}

// djangoManageFirst returns the first non-empty string argument found
// among keys — implementing django_manage's several aliased arguments
// (project_path/app_path/chdir, virtualenv/virtual_env, pythonpath/
// python_path, testrunner/test_runner) without a generic alias
// mechanism elsewhere in this package.
func djangoManageFirst(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := argString(args, k, ""); v != "" {
			return v
		}
	}
	return ""
}
