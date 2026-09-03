package modules

import (
	"context"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGunicorn implements Ansible's `gunicorn` (community.general)
// module: starts gunicorn (always daemonized, `-D`) with the given
// settings, on the target, via `conn`.
//
// Args: app (string, required, alias name) — the WSGI callable module;
// venv (string, alias virtualenv) — if set, runs
// "<venv>/bin/gunicorn" instead of a bare `gunicorn` looked up on
// target PATH; config (string, alias conf) — passed as `-c`; chdir
// (string) — passed as `--chdir`; worker (string, one of sync,
// eventlet, gevent, "tornado " (real gunicorn's own choices list really
// does carry that trailing space), gthread, gaiohttp) — passed as `-k`;
// user (string) — passed as `-u`; pid (string) — a PID file path; if
// unset AND the config file (when given) does not itself already
// mention "pid", a temp path from `conn.TempPath` is used instead so
// this module can still read back gunicorn's own PID (and that temp
// file is removed afterward, mirroring real gunicorn's own remove_pid
// step for the unset-pid case).
//
// Mirrors real gunicorn's own (unusual) success signal: real gunicorn
// treats a non-empty **stderr** from the initial `gunicorn -D ...`
// invocation as the failure signal — NOT the process exit code — since
// `-D` daemonizes and the parent normally returns 0 either way; this
// port replicates that literally rather than "fixing" it to check RC,
// since it is real gunicorn's own shipped (if surprising) behavior. On
// an apparently-clean launch, this port sleeps 0.5s (matching real
// gunicorn's own fixed delay) then reads the PID file; if it never
// appears, this reports Failed using the same config-errorlog vs.
// temp-errorlog fallback real gunicorn's own error path uses.
//
// Simplifications vs real gunicorn: check_mode is not modeled (see
// zfs_delegate_admin.go's own doc comment); config-file content
// inspection ("does it already set errorlog/pid") is done with a
// literal `grep`, matching real gunicorn's own line-substring
// search_existing_config (not a real config-file parse); no attempt is
// made to prefer a venv-relative gunicorn over PATH beyond the explicit
// `venv` argument (real gunicorn's own fallback uses
// `module.get_bin_path`, which has no equivalent here since this port
// never runs Go code on the target).
func moduleGunicorn(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	app, err := requireString(args, "app")
	if err != nil {
		app, err = requireString(args, "name")
		if err != nil {
			return Result{}, errArg("gunicorn: missing required argument: app")
		}
	}
	venv := argString(args, "venv", argString(args, "virtualenv", ""))
	config := argString(args, "config", argString(args, "conf", ""))
	chdir := argString(args, "chdir", "")
	worker := argString(args, "worker", "")
	user := argString(args, "user", "")
	pidArg := argString(args, "pid", "")

	gunicornBin := "gunicorn"
	if venv != "" {
		gunicornBin = venv + "/bin/gunicorn"
	}

	tmpPid := conn.TempPath("gunicorn.temp.pid")
	tmpErrorLog := conn.TempPath("gunicorn.temp.error.log")
	if _, err := run(ctx, conn, "rm -f "+shellQuote(tmpPid)+" "+shellQuote(tmpErrorLog)); err != nil {
		return Result{}, err
	}

	hasErrorlog, err := gunicornConfigHas(ctx, conn, config, "errorlog")
	if err != nil {
		return Result{}, err
	}
	hasPidInConfig, err := gunicornConfigHas(ctx, conn, config, "pid")
	if err != nil {
		return Result{}, err
	}

	pidPath := pidArg
	if pidPath == "" && !hasPidInConfig {
		pidPath = tmpPid
	}

	cmd := shellQuote(gunicornBin) + " -D"
	if config != "" {
		cmd += " -c " + shellQuote(config)
	}
	if chdir != "" {
		cmd += " --chdir " + shellQuote(chdir)
	}
	if worker != "" {
		cmd += " -k " + shellQuote(worker)
	}
	if user != "" {
		cmd += " -u " + shellQuote(user)
	}
	errorLog := tmpErrorLog
	if !hasErrorlog {
		cmd += " --error-logfile " + shellQuote(errorLog)
	}
	if !hasPidInConfig {
		cmd += " --pid " + shellQuote(pidPath)
	}
	cmd += " " + shellQuote(app)

	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return Result{}, err
	}

	if strings.TrimSpace(res.Stderr) == "" {
		time.Sleep(500 * time.Millisecond)
		exists, err := pathExists(ctx, conn, pidPath)
		if err != nil {
			return Result{}, err
		}
		if exists {
			pid, err := run(ctx, conn, "head -n1 "+shellQuote(pidPath))
			if err != nil {
				return Result{}, err
			}
			if pidArg == "" {
				_, _ = run(ctx, conn, "rm -f "+shellQuote(pidPath))
			}
			return Changed("gunicorn started").WithExtra("gunicorn", strings.TrimSpace(pid)).WithExtra("debug", cmd), nil
		}

		errMsg := "Log not found"
		if hasErrorlog {
			errMsg = "Please check your " + config
		} else {
			logExists, err := pathExists(ctx, conn, tmpErrorLog)
			if err != nil {
				return Result{}, err
			}
			if logExists {
				content, _ := run(ctx, conn, "cat "+shellQuote(tmpErrorLog))
				errMsg = content
				_, _ = run(ctx, conn, "rm -f "+shellQuote(tmpErrorLog))
			}
		}
		return Fail("Failed to start gunicorn. " + errMsg), nil
	}

	return Fail("Failed to start gunicorn " + strings.TrimSpace(res.Stderr)), nil
}

// gunicornConfigHas reports whether config (if any) exists on the
// target and contains a line mentioning option — matching real
// gunicorn's own search_existing_config (a plain substring search, not
// a real config-file parse).
func gunicornConfigHas(ctx context.Context, conn remoteexec.Connection, config, option string) (bool, error) {
	if config == "" {
		return false, nil
	}
	res, err := runStatus(ctx, conn, "test -f "+shellQuote(config)+" && grep -q "+shellQuote(option)+" "+shellQuote(config))
	if err != nil {
		return false, err
	}
	return res.RC == 0, nil
}
