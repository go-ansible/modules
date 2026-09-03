package modules

import (
	"context"
	"path"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

type rhsmRepo struct {
	id, name, url string
	enabled       bool
}

// moduleRhsmRepository implements Ansible's `rhsm_repository`
// (community.general) module: enables or disables RHSM-provided yum
// repositories via `subscription-manager repos`.
//
// Args: name ([]string, required — one or more repository IDs; each
// entry may be a plain ID or a glob pattern, matched against every
// currently known repo ID via Go's path.Match, this port's substitute
// for real rhsm_repository's own Python fnmatch — close enough for the
// '*'/'?'/'[...]' patterns typically given, same caveat
// systemd_info.go's own doc comment documents for the same
// substitution); state (enabled|disabled, default enabled — real
// rhsm_repository's own present/absent aliases were removed in
// community.general 10.0.0, so this port, matching the CURRENT real
// module, does not accept them either); purge (bool, default false —
// disable every currently enabled repo NOT matched by name).
//
// Requires root (checked via `id -u` on the target, the same
// substitution rhsm_release.go documents for real rhsm_repository's
// own `os.getuid() != 0` check).
//
// Any name entry matching zero repos fails the whole module
// (Result{Failed:true}), matching real rhsm_repository's own
// `module.fail_json` for that case — even if other entries in the same
// list DID match something.
func moduleRhsmRepository(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	uid, err := run(ctx, conn, "id -u")
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(uid) != "0" {
		return Fail("rhsm_repository: interacting with subscription-manager requires root permissions ('become: true')"), nil
	}

	names := argStringList(args, "name")
	if len(names) == 0 {
		return Result{}, errArg("rhsm_repository: name is required")
	}
	state := argString(args, "state", "enabled")
	if state != "enabled" && state != "disabled" {
		return Result{}, errArg("rhsm_repository: state must be enabled or disabled, got %q", state)
	}
	purge := argBool(args, "purge", false)

	repos, failMsg, err := rhsmListRepos(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail(failMsg), nil
	}

	matched := map[string][]int{} // pattern -> indexes into repos
	for _, pattern := range names {
		for i, r := range repos {
			if ok, _ := path.Match(pattern, r.id); ok {
				matched[pattern] = append(matched[pattern], i)
			}
		}
		if len(matched[pattern]) == 0 {
			return Fail("rhsm_repository: " + pattern + " is not a valid repository ID"), nil
		}
	}

	changed := false
	var rhsmArgs []string
	matchedIdx := map[int]bool{}
	for _, idxs := range matched {
		for _, i := range idxs {
			matchedIdx[i] = true
			if state == "disabled" {
				if repos[i].enabled {
					changed = true
				}
				rhsmArgs = append(rhsmArgs, "--disable", repos[i].id)
				repos[i].enabled = false
			} else {
				if !repos[i].enabled {
					changed = true
				}
				rhsmArgs = append(rhsmArgs, "--enable", repos[i].id)
				repos[i].enabled = true
			}
		}
	}

	if purge {
		for i, r := range repos {
			if r.enabled && !matchedIdx[i] {
				changed = true
				rhsmArgs = append(rhsmArgs, "--disable", r.id)
				repos[i].enabled = false
			}
		}
	}

	if changed {
		cmd := "subscription-manager repos"
		for _, a := range rhsmArgs {
			cmd += " " + shellQuote(a)
		}
		res, err := runStatus(ctx, conn, cmd)
		if err != nil {
			return Result{}, err
		}
		if res.RC == 1 {
			return Fail("rhsm_repository: subscription-manager failed with the following error: " + strings.TrimSpace(res.Stderr)), nil
		}
	}

	repositories := make([]map[string]any, len(repos))
	for i, r := range repos {
		repositories[i] = map[string]any{"id": r.id, "name": r.name, "url": r.url, "enabled": r.enabled}
	}

	result := Ok("repositories unchanged")
	if changed {
		result = Changed("repositories updated")
	}
	return result.WithExtra("repositories", repositories), nil
}

// rhsmListRepos parses `subscription-manager repos --list`, matching
// real rhsm_repository's own list_repositories. A non-empty failMsg
// means real rhsm_repository would module.fail_json here (no err: the
// command ran fine, it just reported a well-formed failure).
func rhsmListRepos(ctx context.Context, conn remoteexec.Connection) (repos []rhsmRepo, failMsg string, err error) {
	res, err := runStatus(ctx, conn, "subscription-manager repos --list")
	if err != nil {
		return nil, "", err
	}
	if res.RC == 0 && res.Stdout == "This system has no repositories available through subscriptions.\n" {
		return nil, "rhsm_repository: this system has no repositories available through subscriptions", nil
	}
	if res.RC == 1 {
		return nil, "rhsm_repository: subscription-manager failed with the following error: " + strings.TrimSpace(res.Stderr), nil
	}

	var id, name, url string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if line == "" || line[0] == '+' || line[0] == ' ' {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Repo ID: "):
			id = strings.TrimSpace(line[len("Repo ID: "):])
		case strings.HasPrefix(line, "Repo Name: "):
			name = strings.TrimSpace(line[len("Repo Name: "):])
		case strings.HasPrefix(line, "Repo URL: "):
			url = strings.TrimSpace(line[len("Repo URL: "):])
		case strings.HasPrefix(line, "Enabled: "):
			enabled := strings.TrimSpace(line[len("Enabled: "):]) == "1"
			repos = append(repos, rhsmRepo{id: id, name: name, url: url, enabled: enabled})
		}
	}
	return repos, "", nil
}
