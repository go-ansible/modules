package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGitConfigInfo implements Ansible's `git_config_info` module: a
// read-only counterpart to git_config.go, reading entirely through
// `git config` itself — reusing git_config.go's own gitConfigBaseCmd so
// both modules compose the exact same "git [-C repo] config
// <scope-flag>" prefix.
//
// Args: name (string, optional) — when omitted, EVERY setting is
// returned; scope (file|local|global|system, default "system"); path
// (string) — required when scope=local (the repo to read from) or
// scope=file (the config file to read from); real git_config_info's own
// single `path` argument doubles for both roles depending on scope,
// matched here by passing it as both gitConfigBaseCmd's repo and file
// parameters (only the one scope actually uses picks it up).
//
// Never Changed — this module only ever reads.
//
// Deviations vs real git_config_info: real git_config_info invokes
// `git config --includes --null --<scope> ...`, changing its OWN
// working directory to path for scope=local rather than passing `-C`;
// this port instead reuses git_config.go's gitConfigBaseCmd, which
// composes `git -C <repo> config --local ...` — functionally
// equivalent output, chosen for consistency with this port's sibling
// git_config module rather than introducing a second, divergent way to
// target a repo. This port also omits `--null` (NUL-delimited records)
// in favor of plain newline-split parsing (via this package's own
// splitLines, the same helper git_config.go itself uses for its
// `--get-all` idempotency check): a config value containing an embedded
// literal newline (rare, but valid git config syntax) is mis-split
// here, exactly as it would be by any of this package's other
// newline-based parsers — a disclosed, shared narrowing, not unique to
// this module. `--includes` (git's own include/includeIf directive
// expansion) IS passed through, since it needs no NUL-delimited output
// to work correctly.
//
// Real git_config_info tolerates two specific `git config` failure
// modes as "no values" rather than failing the task: rc==128 with
// stderr containing "unable to read config file" (the scope's backing
// file does not exist yet — nothing has been set there) and a plain
// unset key (rc==1 from `--get-all`, no stderr). This port matches
// both; any OTHER failure (e.g. a malformed config file) is reported as
// a normal Result{Failed:true}, matching real git_config_info's own
// module.fail_json for rc>=2 with an unrecognized error.
func moduleGitConfigInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name := argString(args, "name", "")
	scope := argString(args, "scope", "system")
	if scope != "file" && scope != "local" && scope != "global" && scope != "system" {
		return Result{}, errArg("git_config_info: scope must be file, local, global, or system, got %q", scope)
	}
	path := argString(args, "path", "")
	if scope == "local" && path == "" {
		return Result{}, errArg("git_config_info: path is required when scope is local")
	}
	if scope == "file" && path == "" {
		return Result{}, errArg("git_config_info: path is required when scope is file")
	}

	base := gitConfigBaseCmd(scope, path, path) + " --includes"

	if name != "" {
		cmd := base + " --get-all " + shellQuote(name)
		values, failMsg, err := gitConfigInfoRun(ctx, conn, cmd)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail("git_config_info: " + failMsg), nil
		}
		firstValue := ""
		if len(values) > 0 {
			firstValue = values[0]
		}
		valuesAny := make([]any, len(values))
		for i, v := range values {
			valuesAny[i] = v
		}
		res := Ok("").WithExtra("config_value", firstValue).
			WithExtra("config_values", map[string]any{name: valuesAny})
		return res, nil
	}

	cmd := base + " --list"
	lines, failMsg, err := gitConfigInfoRun(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if failMsg != "" {
		return Fail("git_config_info: " + failMsg), nil
	}
	configValues := map[string]any{}
	for _, line := range lines {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		existing, _ := configValues[k].([]any)
		configValues[k] = append(existing, v)
	}
	res := Ok("").WithExtra("config_value", "").WithExtra("config_values", configValues)
	return res, nil
}

// gitConfigInfoRun runs cmd and applies real git_config_info's own
// tolerance for an unset key or a not-yet-existing scope file (both
// reported back as an empty values slice, no failure) while surfacing
// any other non-zero exit as a failMsg for the caller to Fail() with.
func gitConfigInfoRun(ctx context.Context, conn remoteexec.Connection, cmd string) (values []string, failMsg string, err error) {
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return nil, "", err
	}
	if res.RC == 0 {
		return splitLines(res.Stdout), "", nil
	}
	if res.RC == 1 || strings.Contains(res.Stderr, "unable to read config file") {
		return nil, "", nil
	}
	return nil, strings.TrimSpace(res.Stderr), nil
}
