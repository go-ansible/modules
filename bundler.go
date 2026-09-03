package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleBundler implements (a subset of) Ansible's `bundler` module:
// runs `bundle install`/`bundle update` for a Ruby project's Gemfile.
//
// Args: state (present|latest, default "present"); chdir (string,
// optional) — directory containing the Gemfile; gemfile (string,
// optional) — explicit Gemfile path, passed as `--gemfile`;
// exclude_groups ([]string, optional) — passed as `--without`
// (colon-joined, matching bundler's own CLI format); deployment_mode
// (bool, default false) — passes `--deployment`; extra_args (string,
// optional) — appended verbatim.
//
// Simplifications vs real bundler: no `binstub_directory`, `clean`,
// `executable`, or `gem_path` support. Unlike this batch's package
// managers, bundler has no cheap way to probe "is the Gemfile already
// satisfied" from shell without parsing `bundle check`'s own success/
// failure semantics across bundler versions — this port always runs
// `bundle install`/`update` and reports changed on success, the same
// "always changed" tradeoff apt.go/dnf.go's own `state: latest` branch
// documents, applied here to `state: present` too since bundler's
// idempotency and its upgrade behavior are both opaque to a shell
// probe.
func moduleBundler(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	chdir := argString(args, "chdir", "")
	gemfile := argString(args, "gemfile", "")
	excludeGroups := argStringList(args, "exclude_groups")
	deploymentMode := argBool(args, "deployment_mode", false)
	extraArgs := argString(args, "extra_args", "")

	verb := "install"
	if state == "latest" {
		verb = "update"
	} else if state != "present" {
		return Result{}, errArg("bundler: state must be present or latest, got %q", state)
	}

	cmd := "bundle " + verb
	if gemfile != "" {
		cmd += " --gemfile " + shellQuote(gemfile)
	}
	if deploymentMode && verb == "install" {
		cmd += " --deployment"
	}
	if len(excludeGroups) > 0 {
		cmd += " --without " + shellQuote(joinColon(excludeGroups))
	}
	if extraArgs != "" {
		cmd += " " + extraArgs
	}
	if chdir != "" {
		cmd = "cd " + shellQuote(chdir) + " && " + cmd
	}

	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed("bundle " + verb), nil
}

func joinColon(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ":"
		}
		out += s
	}
	return out
}
