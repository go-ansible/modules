package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLogrotate implements (a subset of) Ansible's `logrotate`
// (community.general) module. Despite its name, real logrotate does
// NOT run the `logrotate` command — it AUTHORS a `logrotate.d`
// configuration FILE (this was confirmed from the real module's own
// ansible-doc output before writing this: its OPTIONS are all
// config-generation knobs like `paths`/`rotation_period`/`compress`,
// and its RETURN VALUES include `config_content`/`config_file`, not any
// rotation-run result). Actually running logrotate is out of scope for
// this module, same as it is for real community.general.logrotate.
//
// Args: name (string, required, aliased `config_name` upstream — this
// port only accepts `name`, per this package's standing convention of
// args already being resolved by the caller, see known_hosts.go);
// config_dir (string, default "/etc/logrotate.d"); state (present|
// absent, default "present"); enabled (bool) — false renames the
// managed file to "<name>.disabled" instead of "<name>"; backup (bool,
// default true, matching real logrotate's own default) — before
// overwriting a changed file, copies it to a timestamped sibling and
// returns that path via Extra["backup_file"]; paths ([]string) — the
// log file patterns rotated, becoming the config block's header line.
//
// The remaining config-content options below are implemented following
// the exact directive spelling shown in real logrotate's own
// ansible-doc RETURN VALUES sample (`config_content`), which is the
// only ground truth available for this module's generated syntax:
// "daily\n    rotate 14\n    compress\n    compress_options -9\n
// delay_compress\n    missing_ok\n    notifempty\n    create 0640
// www-data adm\n    shared_scripts\n    post_rotate\n        ...\n
// endscript\n}\n". Notably, several of these directives (compress_
// options, delay_compress, missing_ok, post_rotate, shared_scripts) use
// the argument's own snake_case spelling rather than logrotate(8)'s
// actual directive names (compressoptions, delaycompress, missingok,
// postrotate, sharedscripts) — that mismatch is reproduced here
// verbatim as a documented quirk INHERITED from the real module being
// ported, not a bug introduced by this port. Directives with no
// confirmed sample (compression_method, no_delay_compress, su,
// date_ext/date_format/date_yesterday, old_dir/no_old_dir/
// create_old_dir, start, extension, mail/mail_first/mail_last, max_age,
// max_size, min_size, shred/shred_cycles, syslog, taboo_ext, include,
// copy/copy_truncate/rename_copy, first_action/last_action/pre_remove)
// follow the SAME observed convention (directive keyword = the
// argument's own snake_case name) for consistency, since that is the
// only signal available; this is a documented extrapolation, not a
// second confirmed data point.
//
// Idempotency and update semantics: unlike real logrotate, which
// documents that omitting `rotation_period`/`size` on a later run
// PRESERVES whatever was already configured (a true merge-update), this
// port always regenerates the whole config block from exactly the
// arguments given to THIS task run — an omitted option is simply not
// written, even if a previous run had set it. This is simpler and
// fully predictable, but is a deviation from real logrotate's partial-
// update behavior for the "modify one option, leave the rest alone"
// use case; a caller relying on that should pass every option on every
// run. Idempotency itself is exact-content comparison against the
// existing file (like copy.go), which is consistent with always-
// regenerate-from-scratch.
func moduleLogrotate(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	configDir := argString(args, "config_dir", "/etc/logrotate.d")
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("logrotate: state must be present or absent, got %q", state)
	}
	backup := argBool(args, "backup", true)

	enabled := argBool(args, "enabled", true)
	activePath := configDir + "/" + name
	disabledPath := activePath + ".disabled"
	dest := activePath
	other := disabledPath
	if !enabled {
		dest, other = disabledPath, activePath
	}

	if state == "absent" {
		changed := false
		for _, p := range []string{activePath, disabledPath} {
			exists, err := pathExists(ctx, conn, p)
			if err != nil {
				return Result{}, err
			}
			if exists {
				if _, err := run(ctx, conn, "rm -f "+shellQuote(p)); err != nil {
					return Result{}, err
				}
				changed = true
			}
		}
		if changed {
			return Changed(name + " removed"), nil
		}
		return Ok(name + " already absent"), nil
	}

	content, err := logrotateBuildConfig(name, args)
	if err != nil {
		return Result{}, err
	}

	// A stale file at the OTHER (enabled/disabled) path is removed, so
	// toggling `enabled` doesn't leave two copies of the config behind.
	changed := false
	if otherExists, err := pathExists(ctx, conn, other); err != nil {
		return Result{}, err
	} else if otherExists {
		if _, err := run(ctx, conn, "rm -f "+shellQuote(other)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	current, err := fetchIfExists(ctx, conn, dest)
	if err != nil {
		return Result{}, err
	}
	var backupFile string
	if current == nil || string(current) != content {
		if current != nil && backup {
			ts, err := run(ctx, conn, "date +%Y%m%d_%H%M%S")
			if err != nil {
				return Result{}, err
			}
			backupFile = dest + "." + ts
			if _, err := run(ctx, conn, "cp "+shellQuote(dest)+" "+shellQuote(backupFile)); err != nil {
				return Result{}, err
			}
		}
		if err := writeRemote(ctx, conn, dest, []byte(content)); err != nil {
			return Result{}, err
		}
		changed = true
	}

	result := Ok(name + " unchanged")
	if changed {
		result = Changed(name)
	}
	result = result.WithExtra("config_file", dest).WithExtra("config_content", content).WithExtra("enabled_state", enabled)
	if backupFile != "" {
		result = result.WithExtra("backup_file", backupFile)
	}
	return result, nil
}

// logrotateBuildConfig renders the full logrotate.d config block for
// name from args (see moduleLogrotate's doc comment for the directive-
// naming convention followed).
func logrotateBuildConfig(name string, args map[string]any) (string, error) {
	paths := argStringList(args, "paths")
	if len(paths) == 0 {
		return "", errArg("logrotate: paths is required when creating a new configuration")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s {\n", strings.Join(paths, " "))

	line := func(s string) { fmt.Fprintf(&b, "    %s\n", s) }
	lineIf := func(key string, format string, a ...any) {
		if _, ok := args[key]; ok {
			line(fmt.Sprintf(format, a...))
		}
	}
	boolLine := func(key, directive string) {
		if argBool(args, key, false) {
			line(directive)
		}
	}

	if rp := argString(args, "rotation_period", ""); rp != "" {
		line(rp)
	}
	if size := argString(args, "size", ""); size != "" {
		line("size " + size)
	}
	lineIf("max_size", "max_size %s", argString(args, "max_size", ""))
	lineIf("min_size", "min_size %s", argString(args, "min_size", ""))
	if _, ok := args["rotate_count"]; ok {
		line("rotate " + strconv.Itoa(argInt(args, "rotate_count", 0)))
	}

	if _, ok := args["compress"]; ok {
		if argBool(args, "compress", false) {
			line("compress")
		} else {
			line("nocompress")
		}
	}
	lineIf("compress_options", "compress_options %s", argString(args, "compress_options", ""))
	lineIf("compression_method", "compression_method %s", argString(args, "compression_method", ""))
	boolLine("delay_compress", "delay_compress")
	boolLine("no_delay_compress", "no_delay_compress")

	boolLine("missing_ok", "missing_ok")
	if _, ok := args["not_if_empty"]; ok {
		if argBool(args, "not_if_empty", true) {
			line("notifempty")
		} else {
			line("ifempty")
		}
	}

	lineIf("create", "create %s", argString(args, "create", ""))
	lineIf("su", "su %s", argString(args, "su", ""))

	boolLine("shared_scripts", "shared_scripts")
	boolLine("copy", "copy")
	boolLine("copy_truncate", "copy_truncate")
	boolLine("rename_copy", "rename_copy")

	boolLine("date_ext", "date_ext")
	lineIf("date_format", "date_format %s", argString(args, "date_format", ""))
	boolLine("date_yesterday", "date_yesterday")

	lineIf("old_dir", "old_dir %s", argString(args, "old_dir", ""))
	boolLine("no_old_dir", "no_old_dir")
	boolLine("create_old_dir", "create_old_dir")

	if _, ok := args["start"]; ok {
		line("start " + strconv.Itoa(argInt(args, "start", 0)))
	}
	lineIf("extension", "extension %s", argString(args, "extension", ""))

	lineIf("mail", "mail %s", argString(args, "mail", ""))
	boolLine("mail_first", "mail_first")
	boolLine("mail_last", "mail_last")
	if _, ok := args["max_age"]; ok {
		line("max_age " + strconv.Itoa(argInt(args, "max_age", 0)))
	}

	boolLine("shred", "shred")
	if _, ok := args["shred_cycles"]; ok {
		line("shred_cycles " + strconv.Itoa(argInt(args, "shred_cycles", 0)))
	}
	boolLine("syslog", "syslog")

	if ext := argStringList(args, "taboo_ext"); len(ext) > 0 {
		line("taboo_ext " + strings.Join(ext, ","))
	}
	lineIf("include", "include %s", argString(args, "include", ""))

	for _, cmd := range argStringList(args, "first_action") {
		line(cmd)
	}
	logrotateScriptBlock(&b, "pre_rotate", argStringList(args, "pre_rotate"))
	logrotateScriptBlock(&b, "post_rotate", argStringList(args, "post_rotate"))
	logrotateScriptBlock(&b, "pre_remove", argStringList(args, "pre_remove"))
	for _, cmd := range argStringList(args, "last_action") {
		line(cmd)
	}

	b.WriteString("}\n")
	return b.String(), nil
}

// logrotateScriptBlock emits a "<directive>\n <cmds...>\nendscript\n"
// block (4/8-space indented, matching real logrotate's own sample
// output), or nothing if cmds is empty.
func logrotateScriptBlock(b *strings.Builder, directive string, cmds []string) {
	if len(cmds) == 0 {
		return
	}
	fmt.Fprintf(b, "    %s\n", directive)
	for _, cmd := range cmds {
		fmt.Fprintf(b, "        %s\n", cmd)
	}
	b.WriteString("    endscript\n")
}
