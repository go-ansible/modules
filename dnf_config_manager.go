package modules

import (
	"context"
	"regexp"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDnfConfigManager implements (a subset of) Ansible's
// `dnf_config_manager` module: enables or disables repositories via
// `dnf config-manager`.
//
// Args: name (string or []string, default []) — one or more repository
// IDs (e.g. "crb"); state (enabled|disabled, default "enabled").
//
// Simplifications vs real dnf_config_manager: real dnf_config_manager.py
// detects whether the target's `dnf` binary is dnf4 or dnf5 (`dnf
// --version`'s first line) and speaks a different dialect to each —
// dnf4 via `dnf repolist --all --verbose` + `dnf config-manager
// --assumeyes --set-enabled/--set-disabled <ids...>`, dnf5 via `dnf repo
// info --all` + `dnf config-manager setopt <id>.enabled=1/0`. This port
// only implements the dnf4 dialect; on a dnf5-only target both the
// repo-listing parse and the config-manager invocation this module runs
// will not match dnf5's actual output/subcommand shape.
func moduleDnfConfigManager(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	names := argStringList(args, "name")
	state := argString(args, "state", "enabled")
	if state != "enabled" && state != "disabled" {
		return Result{}, errArg("dnf_config_manager: state must be enabled or disabled, got %q", state)
	}

	statesPre, err := dnfRepoStates(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	var toChange []string
	for _, id := range names {
		cur, ok := statesPre[id]
		if !ok {
			return Fail("did not find repo with ID '" + id + "' in dnf repolist --all --verbose"), nil
		}
		if cur != state {
			toChange = append(toChange, id)
		}
	}

	res := Result{Changed: len(toChange) > 0}
	res = res.WithExtra("repo_states_pre", packDnfRepoStates(statesPre))
	res = res.WithExtra("changed_repos", toChange)

	if len(toChange) == 0 {
		res = res.WithExtra("repo_states_post", packDnfRepoStates(statesPre))
		return res, nil
	}

	flag := "--set-enabled"
	if state == "disabled" {
		flag = "--set-disabled"
	}
	if _, err := run(ctx, conn, "dnf config-manager --assumeyes "+flag+" "+quoteAll(toChange)); err != nil {
		return Result{}, err
	}

	statesPost, err := dnfRepoStates(ctx, conn)
	if err != nil {
		return Result{}, err
	}
	for _, id := range toChange {
		if statesPost[id] != state {
			return Fail("dnf config-manager failed to make '" + id + "' " + state), nil
		}
	}
	res = res.WithExtra("repo_states_post", packDnfRepoStates(statesPost))
	return res, nil
}

var (
	dnfRepoIDRE     = regexp.MustCompile(`(?i)^Repo-id\s*:\s*(\S+)$`)
	dnfRepoStatusRE = regexp.MustCompile(`(?i)^(?:Repo-)?status\s*:\s*(disabled|enabled)$`)
)

// dnfRepoStates runs `dnf repolist --all --verbose` and parses its
// "Repo-id : <id>" / "Repo-status : enabled|disabled" line pairs into a
// map, matching real dnf_config_manager.py's own regexes exactly (see
// its REPO_ID_RE/REPO_STATUS_RE).
func dnfRepoStates(ctx context.Context, conn remoteexec.Connection) (map[string]string, error) {
	out, err := run(ctx, conn, "dnf repolist --all --verbose")
	if err != nil {
		return nil, err
	}
	repos := map[string]string{}
	lastRepo := ""
	for _, line := range strings.Split(out, "\n") {
		if m := dnfRepoIDRE.FindStringSubmatch(line); m != nil {
			lastRepo = m[1]
			continue
		}
		if m := dnfRepoStatusRE.FindStringSubmatch(line); m != nil && lastRepo != "" {
			repos[lastRepo] = strings.ToLower(m[1])
			lastRepo = ""
		}
	}
	return repos, nil
}

// packDnfRepoStates reshapes a repo-id->state map into the
// {"enabled": [...], "disabled": [...]} (each sorted) form real
// dnf_config_manager returns for repo_states_pre/repo_states_post.
func packDnfRepoStates(states map[string]string) map[string]any {
	var enabled, disabled []string
	for id, state := range states {
		if state == "enabled" {
			enabled = append(enabled, id)
		} else {
			disabled = append(disabled, id)
		}
	}
	sort.Strings(enabled)
	sort.Strings(disabled)
	return map[string]any{"enabled": enabled, "disabled": disabled}
}
