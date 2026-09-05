package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleHponcfg implements Ansible's `hponcfg` module: runs HP/HPE's own
// `hponcfg` utility — the local iLO/RILOE configuration tool that ships
// on the managed server's own OS (not a separate management-network
// client) — to apply an RIBCL XML configuration file to that server's
// own iLO.
//
// Unlike every other module in this batch, this is NOT a CLI
// substitution: real hponcfg.py (community.general) ALREADY shells out
// to the `hponcfg` binary directly via its own CmdRunner (see
// module_utils/_cmd_runner.py) — this port simply reproduces that exact
// same invocation shape, verified against hponcfg.py's own source before
// writing this file (its `command_args_formats`: `src` -> `-f <path>`,
// `verbose` -> `-v` when true, `minfw` -> `-m <value>`).
//
// Args: path (string, required; aliased src in real hponcfg — this
// port's own caller is expected to have already resolved the alias, per
// this package's own convention of taking already-rendered arguments) —
// the RIBCL XML file's path on the TARGET (this port's Connection
// reaches the target directly; there is no separate control-node-to-
// target copy step the way real Ansible's own module-argument transfer
// implies, since the path is used as-is in the composed command);
// minfw (string, optional) — minimum firmware version, passed as `-m`;
// executable (string, default "hponcfg") — the binary name/path to run;
// verbose (bool, default false) — passed as `-v`.
//
// Real hponcfg.py's own comment is explicit and is reproduced here
// verbatim as this port's own behavior too: "Consider every action a
// change (not idempotent yet!)" — hponcfg has no way to know whether the
// RIBCL file's own directives actually changed anything on the iLO
// (RIBCL is a one-way "apply this" protocol, not a declarative
// state-diff one), so a successful run always reports Changed=true, same
// as upstream.
func moduleHponcfg(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	path := argString(args, "path", argString(args, "src", ""))
	if path == "" {
		return Result{}, errArg("hponcfg: missing required argument: path (aliased src)")
	}
	executable := argString(args, "executable", "hponcfg")
	verbose := argBool(args, "verbose", false)
	minfw := argString(args, "minfw", "")

	cmd := executable + " -f " + shellQuote(path)
	if verbose {
		cmd += " -v"
	}
	if minfw != "" {
		cmd += " -m " + shellQuote(minfw)
	}

	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(hponcfgErrMsg(res)), nil
	}
	return Changed(res.Stdout), nil
}

func hponcfgErrMsg(res remoteexec.Result) string {
	if res.Stderr != "" {
		return res.Stderr
	}
	return res.Stdout
}
