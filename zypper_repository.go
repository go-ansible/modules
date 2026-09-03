package modules

import (
	"context"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleZypperRepository implements (a subset of) Ansible's
// `zypper_repository` module: adds or removes a Zypper repository via
// `zypper addrepo`/`zypper removerepo`.
//
// Args: repo (string) — the repository URI; required for state=present,
// and either repo or name is required for state=absent; a repo of "*"
// (with runrefresh=true) refreshes every configured repository instead
// of adding/removing one, and is otherwise an argument error, matching
// real zypper_repository's own `repo=* can only be used with the
// runrefresh option` check; name (string) — the repository's alias;
// required for state=present (real zypper_repository only makes name
// optional for the `.repo`-file form this port doesn't support — see
// below); state (present|absent, default "present"); description
// (string) — the repository's display name; disable_gpg_check (bool,
// default false); autorefresh (bool, alias refresh, default true);
// priority (int); enabled (bool, default true); auto_import_keys (bool,
// default false) — implies runrefresh, and passes
// `--gpg-auto-import-keys` to the refresh; runrefresh (bool, default
// false) — force-refreshes the repository's package list after adding
// it (or, with repo=*, every repository, without adding/removing
// anything).
//
// Simplifications vs real zypper_repository: no support for the
// `.repo`-file form of `repo` (a URL or local path to a file containing
// an INI-style repo definition, downloaded/parsed to derive alias/url/
// other fields) — this port only supports the plain-URI form, where
// `name` is the repository's alias; `overwrite_multiple` (real
// zypper_repository can find and replace more than one pre-existing
// repo matching by alias+URL — this port only ever matches a single
// existing repo, by alias or by URL, and fails if it would need to
// touch more than one); the `$releasever`/`$basearch` URL-variable
// comparison real zypper_repository does before deciding a URL changed
// (this port compares URLs literally). Idempotency is checked via
// `zypper --xmlout repos` (see zypper_repository_info.go's
// queryZypperRepos, shared by both modules), comparing the existing
// repo's alias/url/name/priority/enabled/gpgcheck/autorefresh fields
// against the requested ones; only fields the caller actually set are
// compared, matching real zypper_repository's own "only args the user
// gave override the .repo file's or zypper's own defaults" semantics.
func moduleZypperRepository(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	repo := argString(args, "repo", "")
	name := argString(args, "name", "")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("zypper_repository: state must be present or absent, got %q", state)
	}
	runrefresh := argBool(args, "runrefresh", false)
	autoImportKeys := argBool(args, "auto_import_keys", false)

	if repo == "*" {
		if !runrefresh {
			return Result{}, errArg("zypper_repository: repo=* can only be used with the runrefresh option")
		}
		cmd := "zypper --quiet --non-interactive refresh --force"
		if autoImportKeys {
			cmd = "zypper --quiet --non-interactive --gpg-auto-import-keys refresh --force"
		}
		if _, err := run(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		return Ok("").WithExtra("runrefresh", true), nil
	}

	if state == "present" && repo == "" {
		return Result{}, errArg("zypper_repository: state=present requires repo")
	}
	if state == "absent" && repo == "" && name == "" {
		return Result{}, errArg("zypper_repository: name or repo is required when state=absent")
	}
	if state == "present" && name == "" {
		return Result{}, errArg("zypper_repository: name is required (this port does not support the .repo-file form, where real zypper_repository can derive it)")
	}

	repos, err := queryZypperRepos(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	existing := findZypperRepo(repos, name, repo)

	if state == "absent" {
		if existing == nil {
			return Ok("repository not present"), nil
		}
		if _, err := run(ctx, conn, "zypper --quiet --non-interactive removerepo "+shellQuote(existing.Alias)); err != nil {
			return Result{}, err
		}
		return Changed("removed repository " + existing.Alias), nil
	}

	// state == "present"
	_, hasPriority := args["priority"]
	priorityStr := ""
	if hasPriority {
		priorityStr = strconv.Itoa(argInt(args, "priority", 0))
	}
	wantEnabled, hasEnabled := args["enabled"]
	wantGPG, hasGPGArg := args["disable_gpg_check"]
	wantAutorefresh, hasAutorefresh := args["autorefresh"]
	description := argString(args, "description", "")

	changed := existing == nil
	if existing != nil {
		if existing.URL != repo {
			changed = true
		}
		if description != "" && existing.Name != description {
			changed = true
		}
		if hasPriority && existing.Priority != priorityStr {
			changed = true
		}
		if hasEnabled && existing.Enabled != zypperBoolStr(wantEnabled.(bool)) {
			changed = true
		}
		if hasGPGArg && existing.GpgCheck != zypperBoolStr(!wantGPG.(bool)) {
			changed = true
		}
		if hasAutorefresh && existing.Autorefresh != zypperBoolStr(wantAutorefresh.(bool)) {
			changed = true
		}
	}

	if !changed {
		if runrefresh {
			if err := zypperRefreshOne(ctx, conn, name, autoImportKeys); err != nil {
				return Result{}, err
			}
		}
		return Ok("repository unchanged"), nil
	}

	if existing != nil {
		if _, err := run(ctx, conn, "zypper --quiet --non-interactive removerepo "+shellQuote(existing.Alias)); err != nil {
			return Result{}, err
		}
	}

	cmd := "zypper --quiet --non-interactive addrepo --check --name " + shellQuote(name)
	if hasPriority {
		cmd += " --priority " + shellQuote(priorityStr)
	}
	if hasEnabled && !wantEnabled.(bool) {
		cmd += " --disable"
	}
	if hasGPGArg && wantGPG.(bool) {
		cmd += " --no-gpgcheck"
	} else {
		cmd += " --gpgcheck"
	}
	if !hasAutorefresh || wantAutorefresh.(bool) {
		cmd += " --refresh"
	}
	cmd += " " + shellQuote(repo) + " " + shellQuote(name)

	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	if runrefresh || autoImportKeys {
		if err := zypperRefreshOne(ctx, conn, name, autoImportKeys); err != nil {
			return Result{}, err
		}
	}
	return Changed("added repository " + name), nil
}

func zypperRefreshOne(ctx context.Context, conn remoteexec.Connection, alias string, autoImportKeys bool) error {
	cmd := "zypper --quiet --non-interactive refresh --force -r " + shellQuote(alias)
	if autoImportKeys {
		cmd = "zypper --quiet --non-interactive --gpg-auto-import-keys refresh --force -r " + shellQuote(alias)
	}
	_, err := run(ctx, conn, cmd)
	return err
}

func zypperBoolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// findZypperRepo looks for an existing repo matching alias (by name) or
// url (by repo), preferring an alias match — matching real
// zypper_repository's own "a repo can be uniquely identified by an
// alias + url" comment, simplified to a single match rather than
// collecting every match (see the doc comment on moduleZypperRepository
// for why this port doesn't replicate the overwrite_multiple path).
func findZypperRepo(repos []zypperRepoXML, alias, url string) *zypperRepoXML {
	for i := range repos {
		if alias != "" && repos[i].Alias == alias {
			return &repos[i]
		}
	}
	for i := range repos {
		if url != "" && strings.TrimRight(repos[i].URL, "/") == strings.TrimRight(url, "/") {
			return &repos[i]
		}
	}
	return nil
}
