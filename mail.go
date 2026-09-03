package modules

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleMail implements (a subset of) Ansible's `mail` module: sends an
// e-mail by composing an SMTP session.
//
// Architectural note (read this before assuming it's another
// synchronize.go-style fail-stub): real community.general.mail is
// implemented with Python's stdlib `smtplib`, which opens its own
// socket to `host:port` from wherever the module process is actually
// running. Unlike ansible.posix.synchronize — which is documented as
// ALWAYS running on the controller and reaching the target through
// rsync/ssh using connection details this port's remoteexec.Connection
// deliberately never exposes — real mail's own doc and examples show it
// running on the TARGET by default: one example is explicitly captioned
// "Sending an e-mail using the remote machine, not the Ansible
// controller node" and uses no `delegate_to` at all, while a *different*
// example adds `delegate_to: localhost` specifically to run it from the
// controller instead. That means, absent delegate_to, real mail's SMTP
// socket originates from the target — exactly the machine this port's
// modules already run their shell composition against via conn.Exec.
// So mail is architecturally COMPATIBLE with this package (a playbook
// author wanting controller-originated mail would need delegate_to
// support at the engine/playbook level, which is a property of every
// module equally and out of scope for one module's own implementation
// to solve). This module is therefore implemented for real, not as a
// fail-stub.
//
// Implementation: this port has no SMTP client of its own to link
// against, so — mirroring htpasswd.go/java_cert.go's own "shell out to
// a real external tool rather than reimplement a protocol/format in Go"
// stance — it composes the SMTP session via the target's own `curl`
// binary, using curl's native (since curl 7.20) `smtp://`/`smtps://`
// support: envelope MAIL FROM/RCPT TO via --mail-from/--mail-rcpt, and
// the full RFC 5322 message (headers + body) built in Go and piped to
// curl over stdin via --upload-file -. `curl` is treated as a hard
// target-side dependency, checked with `command -v curl` and failed
// cleanly (Result{Failed:true}, not a Go error) if missing, matching
// this batch's convention for htpasswd/java_cert's own external tools.
//
// Args: host (default "localhost"); port (int, default 25); sender
// (default "root", aliased from `from` in real mail); to (list,
// default ["root"], aliased from `recipients`); cc, bcc (list, default
// []); subject (default ""); body (default ""); subtype (plain|html,
// default "plain") — sets the message's Content-Type major/minor type;
// charset (default "utf-8"); username, password (SMTP auth, passed to
// curl's --user); secure (always|never|starttls|try, default "try") —
// mapped to curl's own TLS negotiation flags: "always" uses the
// `smtps://` scheme (implicit TLS from connect, matching real mail's
// own use of smtplib.SMTP_SSL for this value); "starttls" adds
// --ssl-reqd (upgrade required, fails if unavailable); "try" adds --ssl
// (upgrade opportunistically, continuing in plaintext if the server
// doesn't offer it); "never" adds neither; timeout (int, default 20,
// seconds) — passed to curl's --max-time, which bounds curl's ENTIRE
// invocation, not just the connection-establishment phase real mail's
// own `timeout` documents bounding — a close but not exact match;
// headers (list of "key=value" strings) — each becomes its own `Key:
// Value` header line in the composed message; message_id_domain
// (default "ansible") — used to build a synthesized Message-ID header.
//
// Bcc recipients ARE included in curl's envelope --mail-rcpt (so the
// message is actually delivered to them) but are deliberately NEVER
// written into the message's own headers — this is not a narrowing,
// it's what "blind" copy means, and it's exactly how real mail (via
// smtplib.sendmail's separate envelope-recipient list) behaves too.
//
// Simplifications vs real mail: no `attach` or `inline` support — real
// mail builds a multipart/mixed (or multipart/related, for inline
// images) MIME message via Python's email library; composing that by
// hand in Go was judged out of scope for this batch, so this port only
// ever sends a single-part text/<subtype> message, and any attach/
// inline arguments given are silently ignored (not validated or
// errored on, matching this package's general convention of only
// reading the arguments a module actually implements); no `ehlohost`
// support (curl's SMTP client has no option to override the local
// hostname/IP literal it presents in its EHLO greeting); the Date
// header is stamped using the CONTROL NODE's clock at the moment this
// Go function runs, not the target's own clock the way real mail's
// on-target smtplib naturally would — a real but usually-immaterial gap
// for a header whose main job is rough chronological ordering; address
// values (sender/to/cc/bcc) are parsed naively — the text between the
// first '<' and '>' if present, else the whole trimmed string — rather
// than a proper RFC 5322 address-list parser, so a malformed or
// unusual address phrase could confuse the envelope extraction. Mail
// sending is inherently non-idempotent (like real mail, which has no
// concept of "already sent"), so this module always reports Changed on
// a successful send.
func moduleMail(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	host := argString(args, "host", "localhost")
	port := argInt(args, "port", 25)
	sender := argString(args, "sender", "root")
	to := argStringList(args, "to")
	if len(to) == 0 {
		to = []string{"root"}
	}
	cc := argStringList(args, "cc")
	bcc := argStringList(args, "bcc")
	subject := argString(args, "subject", "")
	body := argString(args, "body", "")
	subtype := argString(args, "subtype", "plain")
	if subtype != "plain" && subtype != "html" {
		return Result{}, errArg("mail: subtype must be plain or html, got %q", subtype)
	}
	charset := argString(args, "charset", "utf-8")
	username := argString(args, "username", "")
	password := argString(args, "password", "")
	secure := argString(args, "secure", "try")
	switch secure {
	case "always", "never", "starttls", "try":
	default:
		return Result{}, errArg("mail: secure must be always, never, starttls, or try, got %q", secure)
	}
	timeout := argInt(args, "timeout", 20)
	headers := argStringList(args, "headers")
	messageIDDomain := argString(args, "message_id_domain", "ansible")

	if _, err := run(ctx, conn, "command -v curl"); err != nil {
		return Fail("mail: the curl binary is required on the target to compose an SMTP session " +
			"(this port has no SMTP client of its own — see moduleMail's doc comment)"), nil
	}

	envelopeRecipients := make([]string, 0, len(to)+len(cc)+len(bcc))
	envelopeRecipients = append(envelopeRecipients, to...)
	envelopeRecipients = append(envelopeRecipients, cc...)
	envelopeRecipients = append(envelopeRecipients, bcc...)
	if len(envelopeRecipients) == 0 {
		return Result{}, errArg("mail: no recipients (to/cc/bcc are all empty)")
	}

	message := mailBuildMessage(sender, to, cc, subject, body, subtype, charset, headers, messageIDDomain)

	cmd := mailCurlCmd(host, port, sender, envelopeRecipients, username, password, secure, timeout)
	res, err := conn.Exec(ctx, cmd, strings.NewReader(message))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("mail: sending to %s:%d failed: %s", host, port, strings.TrimSpace(res.Stderr))), nil
	}

	return Changed(fmt.Sprintf("sent mail to %d recipient(s) via %s:%d", len(envelopeRecipients), host, port)), nil
}

// mailCurlCmd builds the curl invocation that sends the SMTP session
// (see moduleMail's doc comment for the secure -> TLS-flag mapping).
func mailCurlCmd(host string, port int, sender string, recipients []string, username, password, secure string, timeout int) string {
	scheme := "smtp"
	if secure == "always" {
		scheme = "smtps"
	}
	url := scheme + "://" + host + ":" + strconv.Itoa(port)

	parts := []string{"curl", "--silent", "--show-error", "--url", shellQuote(url)}
	parts = append(parts, "--mail-from", shellQuote("<"+mailExtractAddr(sender)+">"))
	for _, r := range recipients {
		parts = append(parts, "--mail-rcpt", shellQuote("<"+mailExtractAddr(r)+">"))
	}
	if username != "" {
		parts = append(parts, "--user", shellQuote(username+":"+password))
	}
	switch secure {
	case "try":
		parts = append(parts, "--ssl")
	case "starttls":
		parts = append(parts, "--ssl-reqd")
	}
	parts = append(parts, "--max-time", strconv.Itoa(timeout))
	parts = append(parts, "--upload-file", "-")
	return strings.Join(parts, " ")
}

// mailBuildMessage renders the RFC 5322 headers + body piped to curl's
// stdin. bcc recipients are intentionally never passed in here — see
// moduleMail's doc comment on why they must not appear in any header.
func mailBuildMessage(sender string, to, cc []string, subject, body, subtype, charset string, headers []string, messageIDDomain string) string {
	var b strings.Builder
	writeHeader := func(name, value string) {
		if value != "" {
			b.WriteString(name)
			b.WriteString(": ")
			b.WriteString(value)
			b.WriteString("\r\n")
		}
	}

	writeHeader("From", sender)
	writeHeader("To", strings.Join(to, ", "))
	if len(cc) > 0 {
		writeHeader("Cc", strings.Join(cc, ", "))
	}
	writeHeader("Subject", subject)
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	now := time.Now()
	writeHeader("Message-ID", fmt.Sprintf("<%d.%d@%s>", now.Unix(), now.UnixNano()%1_000_000_000, messageIDDomain))
	writeHeader("MIME-Version", "1.0")
	writeHeader("Content-Type", fmt.Sprintf("text/%s; charset=%q", subtype, charset))
	writeHeader("Content-Transfer-Encoding", "8bit")

	for _, h := range headers {
		k, v, ok := strings.Cut(h, "=")
		if !ok {
			continue
		}
		writeHeader(strings.TrimSpace(k), strings.TrimSpace(v))
	}

	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}

// mailExtractAddr pulls a bare address out of an address-with-phrase
// string like "John Smith <john.smith@example.com>" (returning the
// content between the angle brackets), or returns the trimmed input
// unchanged if it has no angle brackets — see moduleMail's doc comment
// on why this is a naive stand-in for a proper RFC 5322 address parser.
func mailExtractAddr(s string) string {
	s = strings.TrimSpace(s)
	start := strings.IndexByte(s, '<')
	end := strings.IndexByte(s, '>')
	if start >= 0 && end > start {
		return strings.TrimSpace(s[start+1 : end])
	}
	return s
}
