package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleComposer implements (a subset of) Ansible's `composer` module:
// runs PHP's Composer dependency manager (`composer install`, `composer
// require`, etc.) for a project, or globally.
//
// Args: command (string, default "install") — composer subcommand
// (install|update|require|remove|create-project|...); arguments (string,
// default "") — raw CLI arguments appended verbatim after the command
// (e.g. a package name for `require`/`remove`), matching real composer's
// own free-form `arguments` string rather than a package-name list;
// working_dir (string) — project directory, passed as `--working-dir`,
// required unless global_command=true; global_command (bool, default
// false) — runs `composer global <command>` instead, ignoring
// working_dir; composer_executable (string, default "composer") — path
// to the composer binary/phar; executable (string, optional) — path to
// the PHP binary used to invoke composer_executable when PHP isn't in
// PATH (real Ansible's `php_path` alias is not implemented here — use
// `executable`); no_dev (bool, default true); no_plugins, no_scripts,
// classmap_authoritative, apcu_autoloader, prefer_dist, prefer_source,
// ignore_platform_reqs (bool, default false); optimize_autoloader (bool,
// default true; forced true when classmap_authoritative=true, matching
// real composer's own documented note that classmap_authoritative
// implicitly enables it); force (bool, default false) — only affects
// command=create-project's idempotency check.
//
// Simplifications vs real composer: composer has no general "is this
// already satisfied" probe for install/update/require/remove, so (like
// bundler.go's own tradeoff) this port always runs the command and
// reports changed on success — except command=create-project, whose
// real module DOES have a documented idempotency check (skip if
// working_dir/composer.json already exists, unless force=true), which
// this port replicates exactly since it's cheap (a single `test -e`).
// The three flags real composer always appends when available
// (--no-ansi --no-interaction --no-progress) are appended
// unconditionally here, without probing composer's own version for
// --no-progress support.
func moduleComposer(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	command := argString(args, "command", "install")
	arguments := argString(args, "arguments", "")
	workingDir := argString(args, "working_dir", "")
	globalCommand := argBool(args, "global_command", false)
	composerBin := argString(args, "composer_executable", "composer")
	phpBin := argString(args, "executable", "")
	noDev := argBool(args, "no_dev", true)
	noPlugins := argBool(args, "no_plugins", false)
	noScripts := argBool(args, "no_scripts", false)
	classmapAuthoritative := argBool(args, "classmap_authoritative", false)
	optimizeAutoloader := argBool(args, "optimize_autoloader", true) || classmapAuthoritative
	apcuAutoloader := argBool(args, "apcu_autoloader", false)
	preferDist := argBool(args, "prefer_dist", false)
	preferSource := argBool(args, "prefer_source", false)
	ignorePlatformReqs := argBool(args, "ignore_platform_reqs", false)
	force := argBool(args, "force", false)

	if !globalCommand && workingDir == "" {
		return Result{}, errArg("composer: working_dir is required unless global_command=true")
	}

	if command == "create-project" && !force && workingDir != "" {
		exists, err := pathExists(ctx, conn, workingDir+"/composer.json")
		if err != nil {
			return Result{}, err
		}
		if exists {
			return Ok("composer.json already exists in " + workingDir), nil
		}
	}

	cmd := composerBin
	if phpBin != "" {
		cmd = phpBin + " " + composerBin
	}
	if globalCommand {
		cmd += " global"
	} else {
		cmd += " --working-dir=" + shellQuote(workingDir)
	}
	cmd += " " + command

	if apcuAutoloader {
		cmd += " --apcu-autoloader"
	}
	if classmapAuthoritative {
		cmd += " --classmap-authoritative"
	}
	if ignorePlatformReqs {
		cmd += " --ignore-platform-reqs"
	}
	if noDev {
		cmd += " --no-dev"
	}
	if noPlugins {
		cmd += " --no-plugins"
	}
	if noScripts {
		cmd += " --no-scripts"
	}
	if optimizeAutoloader {
		cmd += " --optimize-autoloader"
	}
	if preferDist {
		cmd += " --prefer-dist"
	}
	if preferSource {
		cmd += " --prefer-source"
	}
	cmd += " --no-ansi --no-interaction --no-progress"
	if arguments != "" {
		cmd += " " + arguments
	}

	if _, err := run(ctx, conn, cmd); err != nil {
		return Result{}, err
	}
	return Changed("composer " + command), nil
}
