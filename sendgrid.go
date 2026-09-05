package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleSendgrid implements Ansible's `sendgrid` (community.general)
// module: sends an email through a SendGrid account, via Twilio's own
// official `twilio-cli` (github.com/twilio/twilio-cli) — the SAME
// binary twilio.go's own moduleTwilio already shells out to in this
// batch, since Twilio (which owns SendGrid) ships a built-in SendGrid
// integration inside twilio-cli itself, confirmed directly against
// Twilio's own published walkthrough
// (twilio.com/docs/twilio-cli/examples/send-email-sendgrid, fetched
// before writing this file, per this project's own bibliography-
// before-implementing rule): "Twilio CLI features a built-in
// integration with Twilio SendGrid, allowing you to send emails
// directly from your terminal", via the `twilio sendgrid:email:send`
// plugin command, reading the API key from the `SENDGRID_API_KEY`
// environment variable — a DIFFERENT credential shape from
// moduleTwilio's own TWILIO_ACCOUNT_SID/TWILIO_AUTH_TOKEN (SendGrid's
// own API key namespace is separate from a Twilio account's own SID/
// token, even though both are reached through the same `twilio`
// binary), so this file establishes its own env-var wiring rather than
// reusing twilioRequireBinary's auth shape, while still reusing
// twilio.go's own `twilio` binary-presence check
// (twilioRequireBinary), quoting helper (twilioQuoteJoin), and error-
// message helper (twilioErrMsg) — same CLI, same house conventions,
// per this file's own instruction to read twilio.go first.
//
// # CLI syntax, verified against Twilio's own docs page
//
// The docs page's own worked example is exactly:
//
//	twilio email:send \
//	  --to="me@example.com" \
//	  --subject="That cat pic you wanted" \
//	  --text="Look at this fluff: https://unsplash.com/photos/..."
//
// with `--from` shown used only when no default sender is configured
// (`twilio email:set`, an interactive default-setting command this
// port does not drive, matching this project's own "no CLI login/
// config management" convention) — since real sendgrid.py's own
// from_address is a required argument every call of this port
// supplies fresh, `--from` is always sent explicitly here rather than
// relying on any such default. The docs page's own content does not
// show `--html`/`--headers`/`--cc`/`--bcc`/`--attachment` flags for
// `email:send` at all (only `--to`/`--from`/`--subject`/`--text`/
// `--attachment`, the last confirmed by the same page).
//
// # Real sendgrid.py args this port genuinely maps, and the ones it
// # cannot
//
// api_key: real sendgrid.py accepts EITHER api_key OR username+password
// (its own `pip install sendgrid` v2 API-key code path is preferred
// when given); this port always uses SENDGRID_API_KEY (never argv,
// per this project's own hard "no secrets in argv" rule, matching
// moduleTwilio's own TWILIO_AUTH_TOKEN precedent). username/password
// (SendGrid's own older account-credential auth, only usable in real
// sendgrid.py when api_key is absent) have no equivalent in twilio-cli
// at all — `email:send` is documented as SENDGRID_API_KEY-only, so a
// caller supplying username/password without api_key fails cleanly
// rather than being silently ignored. from_address (required) →
// `--from`; from_name — real sendgrid.py's own v2 API concatenates
// this with from_address into a single display-name-plus-address
// field; `email:send` has no separate display-name flag documented, so
// this port folds from_name into the `--from` value the same way
// (`"Name <address>"`), the same shape SMTP/SendGrid's own `from` field
// itself takes. to_addresses (required, one or more) → one `--to` flag
// PER recipient in a single invocation (`email:send` accepts repeated
// `--to`, matching every other twilio-cli list-flag's own documented
// repeatable-flag shape, e.g. `--media-url` in moduleTwilio's sibling
// commands) — unlike moduleTwilio's own one-call-per-recipient shape,
// since sendgrid.py's own v2 API genuinely sends one email to every
// address in a single API call, not one call per recipient. subject
// (required) → `--subject`. body (required) → `--text`, UNLESS
// html_body is true, in which case real sendgrid.py sends body as the
// HTML content instead of plain text — `email:send`'s own docs page
// does not document a `--html` flag at all, so this port cannot
// honestly replicate an HTML-body send through this CLI; html_body=true
// therefore fails cleanly with that explanation rather than silently
// sending as plain text (which would send different content than
// requested). cc/bcc/attachments/headers similarly have no
// corresponding `email:send` flag documented on the one page this
// port's own research could verify against, and each fails cleanly,
// by name, when given a non-empty value — this project's own "if real
// behavior can't be replicated through this port's architecture,
// document that honestly rather than faking it" rule applied directly,
// not an oversight.
//
// Deviation — non-idempotent, matching real sendgrid.py's own NOTES
// verbatim ("This module is non-idempotent because it sends an email
// through the external API. It is idempotent only in the case that the
// module fails."), the same shape as moduleTwilio's own doc comment:
// this port always reports Changed=true on success.
func moduleSendgrid(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "sendgrid"
	apiKey := argString(args, "api_key", "")
	username := argString(args, "username", "")
	password := argString(args, "password", "")
	if apiKey == "" {
		if username == "" || password == "" {
			return Result{}, errArg("%s: missing required argument: api_key (or username and password)", mod)
		}
		return Fail(mod + ": username/password auth has no equivalent in twilio-cli's `email:send` command, which " +
			"is documented as SENDGRID_API_KEY-only — supply api_key instead; see moduleSendgrid's own doc comment"), nil
	}
	fromAddress, err := requireString(args, "from_address")
	if err != nil {
		return Result{}, err
	}
	subject, err := requireString(args, "subject")
	if err != nil {
		return Result{}, err
	}
	body, err := requireString(args, "body")
	if err != nil {
		return Result{}, err
	}
	toAddresses := argStringList(args, "to_addresses")
	if len(toAddresses) == 0 {
		return Result{}, errArg("%s: missing required argument: to_addresses", mod)
	}

	for _, unsupported := range []string{"cc", "bcc", "attachments", "headers"} {
		if v, ok := args[unsupported]; ok && !isEmptyArg(v) {
			return Fail(mod + ": `" + unsupported + "` has no equivalent in twilio-cli's `email:send` command " +
				"(not among its documented flags) — this port cannot honestly replicate it; see moduleSendgrid's " +
				"own doc comment"), nil
		}
	}
	if argBool(args, "html_body", false) {
		return Fail(mod + ": html_body=true has no equivalent in twilio-cli's `email:send` command (no `--html` " +
			"flag is documented) — this port cannot honestly send body as HTML through it; see moduleSendgrid's " +
			"own doc comment"), nil
	}

	if res, ok := twilioRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}

	from := fromAddress
	if fromName := argString(args, "from_name", ""); fromName != "" {
		from = fromName + " <" + fromAddress + ">"
	}

	argv := []string{"email:send", "--from", from, "--subject", subject, "--text", body}
	for _, to := range toAddresses {
		argv = append(argv, "--to", to)
	}
	cmd := "SENDGRID_API_KEY=" + shellQuote(apiKey) + " twilio " + twilioQuoteJoin(argv)
	res, err := conn.Exec(ctx, cmd, nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(mod + ": failed to send email: " + twilioErrMsg(res)), nil
	}
	return Changed(""), nil
}

func isEmptyArg(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []string:
		return len(t) == 0
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	default:
		return false
	}
}
