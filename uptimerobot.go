package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleUptimerobot implements Ansible's `uptimerobot`
// (community.general, DEPRECATED upstream since it targets UptimeRobot's
// retired API v1 — removal scheduled for community.general 15.0.0)
// module: pauses or starts (resumes) one monitor, via UptimeRobot's own
// official CLI, `uptimerobot` (github.com/uptimerobot/uptimerobot-cli,
// npm package `@uptimerobot/cli`, "An official command line interface
// (CLI) for uptimerobot.com", released ~August 2026 covering API v3's
// complete surface with 70+ commands) — instead of real uptimerobot.py's
// own hand-rolled `fetch_url` GET against
// `api.uptimerobot.com/getMonitors?...`/`editMonitor?...` (API v1,
// long retired server-side, per the module's own deprecation notice).
//
// # CLI syntax, verified against uptimerobot-cli's own README (fetched
// # directly from GitHub, not guessed from the module or package name)
//
//   - Get one monitor: `uptimerobot monitors get <id> --json` — used
//     here for real uptimerobot.py's own checkID() equivalent (confirm
//     the monitor exists / the API key is valid before acting). A
//     missing monitor answers with a JSON error object on STDERR
//     (`{"error":{"code":"HTTP_404",...}}`) and a non-zero exit
//     (confirmed exit code 6 for "not found" specifically, verified
//     directly in the CLI's own README, not inferred from HTTP status
//     conventions).
//   - Pause: `uptimerobot monitors bulk pause <id> --confirm` — `bulk`
//     is the CLI's own real subcommand name even for a single monitor
//     ID (confirmed by two independent fetches of the CLI's own
//     README, no non-bulk singular alternative documented); `--confirm`
//     is required non-interactively for what the CLI's own docs
//     classify as a destructive bulk operation.
//   - Start (resume): `uptimerobot monitors bulk start <id> --confirm`,
//     same shape.
//
// # Auth: `--api-key` flag or `UPTIMEROBOT_API_KEY` env var, verified
//
// uptimerobot-cli's own README documents three credential sources in
// priority order: `--api-key` flag, `UPTIMEROBOT_API_KEY` environment
// variable, or a prior `uptimerobot auth login`'s own stored credential
// (OS keyring or `~/.config/uptimerobot/credentials.json`). Per this
// project's hard "no secrets in argv" rule, this port always uses the
// environment-variable form (`UPTIMEROBOT_API_KEY=<key>`, shell-quoted,
// scoped to the single invocation), never `--api-key` — matching
// twilio.go's own TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN precedent
// exactly, and confirmed as a genuine, documented alternative (not
// assumed) for this specific CLI.
//
// Args: apikey (required); monitorid (required); state (required,
// `started`|`paused`).
//
// Deviation — matching a real, verified quirk in uptimerobot.py's own
// source rather than a Go port shortcut: main() never passes `changed`
// to `module.exit_json()` at all (only `msg`/`result`), and Ansible
// defaults an omitted `changed` to false — so the REAL module reports
// Changed=false on every successful run, pause or start alike, despite
// genuinely calling editMonitor every time. This port reproduces that
// exact quirk: Changed is always false on success (only Failed can be
// true, when the CLI itself reports an error), even though the pause/
// start action always genuinely runs — this is not a this-port
// idempotency check, it is the same absence of one the real module
// itself has.
func moduleUptimerobot(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "uptimerobot"
	apikey, err := requireString(args, "apikey")
	if err != nil {
		return Result{}, err
	}
	monitorID, err := requireString(args, "monitorid")
	if err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "started" && state != "paused" {
		return Result{}, errArg("%s: state must be one of [started, paused], got %q", mod, state)
	}

	if res, ok := uptimerobotRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	env := "UPTIMEROBOT_API_KEY=" + shellQuote(apikey) + " "

	getCmd := env + "uptimerobot " + uptimerobotQuoteJoin([]string{"monitors", "get", monitorID, "--json"})
	getRes, err := conn.Exec(ctx, getCmd, nil)
	if err != nil {
		return Result{}, err
	}
	if getRes.RC != 0 {
		return Fail(mod + ": failed to check monitor " + monitorID + ": " + uptimerobotErrMsg(getRes)), nil
	}

	action := "pause"
	if state == "started" {
		action = "start"
	}
	actionCmd := env + "uptimerobot " + uptimerobotQuoteJoin([]string{"monitors", "bulk", action, monitorID, "--confirm"})
	actionRes, err := conn.Exec(ctx, actionCmd, nil)
	if err != nil {
		return Result{}, err
	}
	if actionRes.RC != 0 {
		return Fail(mod + ": failed to " + action + " monitor " + monitorID + ": " + uptimerobotErrMsg(actionRes)), nil
	}

	// See this function's own doc comment: real uptimerobot.py never
	// reports changed=true, on any successful run.
	return Ok("success"), nil
}

func uptimerobotRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v uptimerobot"); err != nil {
		return Fail(fmt.Sprintf("%s: the uptimerobot binary (UptimeRobot's own official CLI, @uptimerobot/cli) "+
			"is required on the target and was not found in PATH — this port shells out to it rather than "+
			"calling UptimeRobot's retired API v1 directly; see moduleUptimerobot's own doc comment",
			moduleName)), false
	}
	return Result{}, true
}

func uptimerobotQuoteJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

func uptimerobotErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
