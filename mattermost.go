package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMattermost implements (a subset of) Ansible's `mattermost`
// module: posts a message to a Mattermost channel, via Mattermost's own
// official `mmctl` CLI instead of real mattermost.py's own Incoming
// Webhook HTTP POST — the same "shell out to the platform's own official
// CLI instead of an API client" precedent this port already uses
// elsewhere (see github_common.go's own doc comment for the fuller
// rationale).
//
// # A real architectural mismatch, not just a missing flag
//
// Real mattermost.py authenticates via an Incoming Webhook's own
// `api_key` (the last path segment of a webhook URL created through
// Mattermost's own Integrations UI) — a per-channel, tokenless posting
// credential with no associated user identity at all. `mmctl post
// create` (verified against mmctl's own generated command reference,
// not guessed from the module name) has NO webhook concept whatsoever:
// it posts as whatever real Mattermost USER SESSION `mmctl` is already
// authenticated as (via a prior `mmctl auth login` — a server URL plus
// either a login/password or a personal access token, stored in
// mmctl's own config, exactly the same shape of narrowing
// gitlab_common.go's own doc comment documents for `glab auth login`).
// So, for this module:
//   - api_key/url/icon_url/username/validate_certs are all accepted
//     (for argument-shape compatibility with real playbooks written
//     against real mattermost.py) but have NO EFFECT on this port's
//     behavior — none of them is a `mmctl` concept. In particular,
//     username/icon_url (a webhook's own per-post identity override)
//     cannot be replicated at all: an `mmctl`-posted message always
//     shows the identity of whatever user `mmctl auth login`
//     authenticated as. This is a deliberate, honestly-documented gap,
//     not a silent misinterpretation.
//   - `mmctl` must already be authenticated on the target (a prior
//     `mmctl auth login <server> --access-token <token>`, or an
//     equivalent login/password flow) before this module runs — this
//     port does not drive that login itself, matching every other
//     CLI-substitution module in this project.
//
// # channel must be given as "team:channel" for this port
//
// Real mattermost.py's own `channel` argument is a bare channel name,
// resolved server-side against whatever team the target webhook itself
// is scoped to (a webhook has no separate "team" concept exposed to the
// caller). `mmctl post create` addresses a channel as `team:channel`
// (verified against mmctl's own generated reference) and has no
// "default team" to fall back on. This port therefore requires channel
// to already be given in `team:channel` form — a documented deviation,
// not a bug: a bare channel name (or an empty channel, which real
// mattermost.py accepts to mean "the webhook's own default channel", a
// concept `mmctl` has no equivalent of) fails with a clear
// argument-validation error rather than guessing at a team.
//
// # attachments and priority are not supported
//
// `mmctl post create` has exactly two flags: `-m/--message` and
// `-r/--reply-to` (verified against its own generated reference) — no
// equivalent of real mattermost.py's own `attachments` (a list of rich
// message-attachment dicts) or `priority` (important|urgent). A
// playbook giving attachments with no text is failed cleanly (real
// mattermost.py requires one of text/attachments; this port only ever
// has a text-shaped path to fall back on). priority is accepted for
// argument-shape compatibility but has no effect.
//
// # Idempotency
//
// Posting a message is inherently not idempotent — matching real
// mattermost.py, which always reports changed=true, this port does the
// same: every successful `mmctl post create` invocation is a Changed
// result.
func moduleMattermost(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	text := argString(args, "text", "")
	if strings.TrimSpace(text) == "" {
		if _, hasAttachments := args["attachments"]; hasAttachments {
			return Fail("mattermost: attachments are not supported by this port's mmctl-based substitution " +
				"(mmctl post create has no attachment flag) — provide text instead"), nil
		}
		return Result{}, errArg("mattermost: missing required argument: text")
	}
	channel, err := requireString(args, "channel")
	if err != nil {
		return Result{}, errArg("mattermost: channel is required by this port (in \"team:channel\" form) — real "+
			"mattermost.py's own default-channel-from-webhook fallback has no mmctl equivalent: %v", err)
	}
	if !strings.Contains(channel, ":") {
		return Result{}, errArg("mattermost: channel must be given as \"team:channel\" for this port's mmctl-based "+
			"substitution, got %q", channel)
	}

	if _, err := run(ctx, conn, "command -v mmctl"); err != nil {
		return Fail("mattermost: the mmctl binary (Mattermost's own official CLI) is required on the target and " +
			"was not found in PATH — see mattermost.go's own doc comment, including the precondition that " +
			"`mmctl auth login` must already have been run on the target"), nil
	}

	res, err := conn.Exec(ctx, mmctlCmd(channel, text), nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail(fmt.Sprintf("mattermost: mmctl post create failed: %s", msg)), nil
	}
	return Changed("message posted to " + channel), nil
}

func mmctlCmd(channel, text string) string {
	return "mmctl post create " + shellQuote(channel) + " --message " + shellQuote(text)
}
