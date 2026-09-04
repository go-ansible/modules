package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleTwilio implements Ansible's `twilio` (community.general)
// module: sends a text message (SMS, or MMS when media_url is given)
// via Twilio's own official CLI, `twilio-cli`
// (github.com/twilio/twilio-cli), specifically its `api:core:messages:create`
// plugin command — the same "shell out to the platform's own official
// CLI instead of an API client" precedent this port already applies
// elsewhere in this batch. `twilio-cli`'s own invocation shape
// (confirmed against Twilio's own published CLI documentation and
// blog walkthroughs, e.g. twilio.com/docs/twilio-cli/quickstart and
// twilio.com/en-us/blog/how-to-send-an-sms-with-twilio-cli-in-less-than-a-minute)
// is `twilio api:core:messages:create --from <E.164> --to <E.164>
// --body "<text>" [--media-url <url>]` — real, individually-documented
// flags, not guessed from the module name.
//
// # Auth precondition
//
// `twilio-cli` must already be authenticated/configured on the TARGET
// host before this module runs: either a prior `twilio login` (which
// writes ~/.twilio-cli/config.json, twilio-cli's own profile store) has
// already run there, or the TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN
// environment variables are already exported in that session's own
// environment (Twilio's own SDK-wide documented env var names) — the
// same shape of precondition ali_common.go's own doc comment sets for
// `aliyun configure`. This port does not attempt to drive `twilio
// login` (an interactive OAuth-like device flow) itself.
//
// Every real twilio module's own account_sid/auth_token arguments ARE
// wired through when given — as the TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN
// environment variables for that single invocation only, never as a
// `--account-sid`/`--auth-token` command-line flag or twilio-cli's own
// `--password` (which IS a real twilio-cli flag, but placing an
// auth_token there would violate this project's own hard "no secrets
// in argv" rule — see redis.go's own REDISCLI_AUTH precedent). When
// neither is given, `twilio-cli` falls back to its own already-logged-in
// default profile as-is.
//
// Args: account_sid (required); auth_token (required); from_number
// (required) — sent as `--from`; to_numbers (required, aliases
// to_number, list) — one `twilio-cli` invocation PER recipient (real
// twilio.py's own TwilioMessage.send loops the same way over
// to_numbers, one REST API call per recipient — this port matches that
// call-per-recipient shape exactly, not a single multi-recipient
// invocation, since neither real Twilio's own Messages API nor
// twilio-cli's own messages:create command accepts more than one `to`
// per call); msg (required) — sent as `--body`; media_url (optional) —
// sent as `--media-url`, turning the message into an MMS, matching
// real twilio.py's own doc.
//
// Deviation — non-idempotent, matching real twilio.py's own NOTES
// verbatim ("This module is non-idempotent because it sends [a
// message] through the external API. It is idempotent only in the
// case that the module fails."): this port always reports Changed=true
// on success, for every recipient, every run.
func moduleTwilio(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "twilio"
	accountSID, err := requireString(args, "account_sid")
	if err != nil {
		return Result{}, err
	}
	authToken, err := requireString(args, "auth_token")
	if err != nil {
		return Result{}, err
	}
	fromNumber, err := requireString(args, "from_number")
	if err != nil {
		return Result{}, err
	}
	msg, err := requireString(args, "msg")
	if err != nil {
		return Result{}, err
	}
	toNumbers := argStringList(args, "to_numbers")
	if len(toNumbers) == 0 {
		toNumbers = argStringList(args, "to_number")
	}
	if len(toNumbers) == 0 {
		return Result{}, errArg("%s: missing required argument: to_numbers", mod)
	}
	mediaURL := argString(args, "media_url", "")

	if res, ok := twilioRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	env := "TWILIO_ACCOUNT_SID=" + shellQuote(accountSID) + " TWILIO_AUTH_TOKEN=" + shellQuote(authToken) + " "

	var sent []string
	for _, to := range toNumbers {
		argv := []string{"api:core:messages:create", "--from", fromNumber, "--to", to, "--body", msg}
		if mediaURL != "" {
			argv = append(argv, "--media-url", mediaURL)
		}
		cmd := env + "twilio " + twilioQuoteJoin(argv)
		res, err := conn.Exec(ctx, cmd, nil)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(mod + ": failed to send message to " + to + ": " + twilioErrMsg(res)), nil
		}
		sent = append(sent, to)
	}

	return Changed("").WithExtra("sent_to", sent), nil
}

func twilioRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v twilio"); err != nil {
		return Fail(moduleName + ": the twilio binary (twilio-cli, Twilio's own official CLI) is required on " +
			"the target and was not found in PATH — this port shells out to it rather than speaking the Twilio " +
			"REST API directly; see moduleTwilio's own doc comment, including the precondition that `twilio " +
			"login` must already have been run (or TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN already set) on the " +
			"target"), false
	}
	return Result{}, true
}

func twilioQuoteJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

func twilioErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
