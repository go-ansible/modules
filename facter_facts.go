package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleFacterFacts implements Ansible's `facter_facts` module: runs
// Puppet's real `facter` discovery program on the target and returns its
// JSON output as facts, read from real facter_facts.py's own source
// (plugins/modules/facter_facts.py) — a thin wrapper: `facter --json
// [arguments...]`, parsed and returned as `ansible_facts.facter`.
//
// Args: arguments (list of string, optional) — extra CLI arguments
// passed straight through to `facter` after `--json` (e.g. `-p` plus a
// list of specific fact names to restrict output to, matching real
// facter_facts' own EXAMPLES).
//
// Binary resolution matches real facter_facts' own
// `module.get_bin_path("facter", opt_dirs=["/opt/puppetlabs/bin"])`:
// PATH is checked first, then the fixed `/opt/puppetlabs/bin/facter`
// fallback (Puppet's own default install location on many platforms,
// often not on PATH); Fail()s cleanly (Result{Failed:true}, not a Go
// error) if neither is found, matching this port's own "external tool
// not installed" convention used throughout (e.g. ipaRequireBinary,
// pip_package_info.go).
//
// Never Changed — this module only ever reads (matching real
// facter_facts' own supports_check_mode with no state-changing action).
//
// Deviation vs real facter_facts: this port does not set
// LANGUAGE=C/LC_ALL=C the way real facter_facts does
// (`module.run_command_environ_update`) — remoteexec.Connection's Exec
// has no per-call environment parameter, so this port prefixes the
// command with a plain `env LANGUAGE=C LC_ALL=C` instead (the same
// portable shell idiom django_command.go already uses for its own
// LC_ALL override), which has the identical effect for a POSIX target.
func moduleFacterFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	bin, err := facterOhaiFindBin(ctx, conn, "facter", "/opt/puppetlabs/bin/facter")
	if err != nil {
		return Result{}, err
	}
	if bin == "" {
		return Fail("facter_facts: the facter binary (from Puppet, https://github.com/puppetlabs/facter) is " +
			"required on the target and was not found in PATH or /opt/puppetlabs/bin"), nil
	}

	cmdParts := []string{"env", "LANGUAGE=C", "LC_ALL=C", bin, "--json"}
	cmdParts = append(cmdParts, argStringList(args, "arguments")...)
	quoted := make([]string, len(cmdParts))
	for i, p := range cmdParts {
		quoted[i] = shellQuote(p)
	}
	res, err := runStatus(ctx, conn, strings.Join(quoted, " "))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail("facter_facts: facter: " + msg), nil
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &parsed); err != nil {
		return Fail("facter_facts: parsing facter's JSON output: " + err.Error()), nil
	}
	return Result{Msg: "gathered facter facts", Facts: map[string]any{"facter": parsed}}, nil
}

// facterOhaiFindBin resolves a binary the way real facter_facts/ohai
// each do: PATH first, then a fixed fallback path — shared by
// facter_facts.go and ohai.go (ohai passes "" for the fallback, since
// real ohai.py has none and just calls `/usr/bin/env ohai`).
func facterOhaiFindBin(ctx context.Context, conn remoteexec.Connection, name, fallback string) (string, error) {
	if _, err := run(ctx, conn, "command -v "+shellQuote(name)); err == nil {
		return name, nil
	}
	if fallback == "" {
		return "", nil
	}
	exists, err := pathExists(ctx, conn, fallback)
	if err != nil {
		return "", err
	}
	if exists {
		return fallback, nil
	}
	return "", nil
}
