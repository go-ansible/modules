package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDjangoDumpData implements Ansible's `django_dumpdata` module:
// runs `django-admin dumpdata` (as `python -m django dumpdata`) to
// serialize the contents of the database, or a subset of it, to a
// file.
//
// Args: settings (string, required); fixture (string, required; alias
// output) — passed as `--output`, the destination file (may end in
// .bz2/.gz/.lzma/.xz for real django-admin to compress it, this port
// does not interpret that suffix itself, matching real
// django_dumpdata, which also leaves the compression choice entirely
// to django-admin). all (bool, optional) — `--all`. format (string,
// default "json", one of xml|json|jsonl|yaml) — `--format`. indent
// (int, optional) — `--indent`. natural_foreign/natural_primary (bool,
// optional) — `--natural-foreign`/`--natural-primary`. primary_keys
// ([]string, optional; alias pks) — `--pks <comma-joined list>`,
// matching real django_dumpdata's own single-flag, comma-joined
// encoding (not one flag per key). excludes ([]string, optional) — one
// `--exclude` flag per value. database (string, default "default") —
// `--database`. apps_models ([]string, optional) — appended as
// positional arguments. pythonpath, skip_checks, traceback, venv,
// verbosity — shared with django_check.go.
//
// Real django_dumpdata has NO check_mode support (attributes.
// check_mode.support: none) and is explicitly documented as not
// idempotent by real Ansible itself (its own NOTE: "the module is not
// idempotent" — dumping data is inherently a side-effecting read with
// no cheap way to tell "the destination already matches" apart without
// dumping it first) — this port reports Changed on every successful
// run, matching that same real, documented behavior rather than
// inventing an idempotency check real django_dumpdata itself disclaims.
func moduleDjangoDumpData(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	settings, err := requireString(args, "settings")
	if err != nil {
		return Result{}, err
	}
	fixture := djangoManageFirst(args, "fixture", "output")
	if fixture == "" {
		return Result{}, errArg("django_dumpdata: missing required argument: fixture (or output)")
	}
	all := argBool(args, "all", false)
	format := argString(args, "format", "json")
	switch format {
	case "xml", "json", "jsonl", "yaml":
	default:
		return Result{}, errArg("django_dumpdata: format must be one of xml, json, jsonl, yaml, got %q", format)
	}
	var indent *int
	if _, ok := args["indent"]; ok {
		n := argInt(args, "indent", 0)
		indent = &n
	}
	naturalForeign := argBool(args, "natural_foreign", false)
	naturalPrimary := argBool(args, "natural_primary", false)
	pks := argStringList(args, "primary_keys")
	if len(pks) == 0 {
		pks = argStringList(args, "pks")
	}
	excludes := argStringList(args, "excludes")
	database := argString(args, "database", "default")
	appsModels := argStringList(args, "apps_models")

	var extra []string
	if all {
		extra = append(extra, "--all")
	}
	extra = append(extra, "--format", format)
	if indent != nil {
		extra = append(extra, "--indent", strconv.Itoa(*indent))
	}
	for _, e := range excludes {
		extra = append(extra, "--exclude", e)
	}
	extra = append(extra, "--database", database)
	if naturalForeign {
		extra = append(extra, "--natural-foreign")
	}
	if naturalPrimary {
		extra = append(extra, "--natural-primary")
	}
	if len(pks) > 0 {
		extra = append(extra, "--pks", strings.Join(pks, ","))
	}
	extra = append(extra, "--output", fixture)
	extra = append(extra, appsModels...)

	return djangoAdminRun(ctx, conn, args, "dumpdata", settings, extra)
}
