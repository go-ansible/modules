package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDjangoLoadData implements Ansible's `django_loaddata` module:
// runs `django-admin loaddata` (as `python -m django loaddata`) to
// load the contents of one or more fixture files into the database.
//
// Args: settings (string, required); fixtures ([]string, optional) —
// appended as positional arguments (the fixture file paths); database
// (string, default "default") — `--database`; ignore_non_existent
// (bool, optional) — `--ignorenonexistent`; app (string, optional) —
// `--app`; format (string, default "json", one of xml|json|jsonl|yaml)
// — `--format`; excludes ([]string, optional) — one `--exclude` flag
// per value; pythonpath, skip_checks, traceback, venv, verbosity —
// shared with django_check.go.
//
// Real django_loaddata has NO check_mode support and is explicitly
// documented as not idempotent, for the same reason as its sibling
// django_dumpdata — see django_dumpdata.go's own doc comment for the
// full quote from real Ansible's own NOTE. This port reports Changed
// on every successful run, matching that same real, documented
// behavior.
func moduleDjangoLoadData(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	settings, err := requireString(args, "settings")
	if err != nil {
		return Result{}, err
	}
	database := argString(args, "database", "default")
	ignoreNonExistent := argBool(args, "ignore_non_existent", false)
	app := argString(args, "app", "")
	format := argString(args, "format", "json")
	switch format {
	case "xml", "json", "jsonl", "yaml":
	default:
		return Result{}, errArg("django_loaddata: format must be one of xml, json, jsonl, yaml, got %q", format)
	}
	excludes := argStringList(args, "excludes")
	fixtures := argStringList(args, "fixtures")

	extra := []string{"--database", database}
	if ignoreNonExistent {
		extra = append(extra, "--ignorenonexistent")
	}
	if app != "" {
		extra = append(extra, "--app", app)
	}
	extra = append(extra, "--format", format)
	for _, e := range excludes {
		extra = append(extra, "--exclude", e)
	}
	extra = append(extra, fixtures...)

	return djangoAdminRun(ctx, conn, args, "loaddata", settings, extra)
}
