package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDjangoCreateCacheTable implements Ansible's
// `django_createcachetable` module: runs `django-admin
// createcachetable` (as `python -m django createcachetable`) to create
// the database table used by Django's database cache backend.
//
// Args: settings (string, required); database (string, default
// "default") — passed as `--database <database>` (always emitted, even
// for the default database, matching real django_createcachetable's
// own `database_dash` var, which is never omitted; real
// django_createcachetable emits it as `--database=<database>`, an
// equals-sign form django-admin's own argparse accepts identically to
// the space-separated form this port uses everywhere else — see
// django_manage.go's own `--database` handling for the same
// convention); pythonpath,
// skip_checks, traceback, venv, verbosity — shared with
// django_check.go (see djangoAdminArgv/djangoAdminRun there).
//
// Real django_createcachetable's own django_admin_arg_order also lists
// a "noinput" flag ahead of "database_dash", but createcachetable is
// never given a `noinput`-typed argument anywhere in its own
// argument_spec (real `--noinput` there is a leftover of a formatter
// table shared with other django_manage-style commands, not a
// documented django_createcachetable option) — this port does not
// invent one either, matching real django-admin's own `createcachetable
// --help`, which has no `--noinput` flag.
//
// Real django_createcachetable also supports check_mode by mapping it
// to `--dry-run`; this port has no check_mode support at all (a
// runtime-engine concern outside every module's own Func signature
// here, not specific to this module — see apache2_mod_proxy.go's own
// documented note for the same gap), so `--dry-run` is never emitted.
func moduleDjangoCreateCacheTable(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	settings, err := requireString(args, "settings")
	if err != nil {
		return Result{}, err
	}
	database := argString(args, "database", "default")

	extra := []string{"--database", database}

	return djangoAdminRun(ctx, conn, args, "createcachetable", settings, extra)
}
