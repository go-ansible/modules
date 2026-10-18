package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGrove implements Ansible's `grove` (community.general) module:
// posts a message to a Grove.io channel.
//
// Grove has no official CLI, so this posts to the same endpoint real
// grove.py's own fetch_url call hits -- POST
// https://grove.io/api/notice/<channel_token>/, form-encoded -- through
// curl ON THE TARGET, where real's own module also runs.
//
// Args: channel_token (required); message_content (required); service
// (default "ansible"); url; icon_url. Wire field names are real's:
// service, message, url, icon_url.
//
// # The URL is the credential here
//
// channel_token is no_log=True in real's argument_spec, and it sits in
// the URL PATH rather than in the body. curlDo sends the url on stdin
// through curl's own -K - config for exactly this reason, so the token
// cannot reach argv even though it is part of the address.
//
// Real reports msg="OK" with changed unset -- posting a notification is
// not a change -- and fails with "failed to send notification: " plus
// fetch_url's own info message. Both are reproduced; the status and
// body go in Extra, since real's info message has no counterpart here.
func moduleGrove(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	token, err := requireString(args, "channel_token")
	if err != nil {
		return Result{}, err
	}
	message, err := requireString(args, "message_content")
	if err != nil {
		return Result{}, err
	}

	if res, ok := requireCurl(ctx, conn, "grove", "Grove.io"); !ok {
		return res, nil
	}

	fields := [][2]string{
		{"service", argString(args, "service", "ansible")},
		{"message", message},
	}
	if u := argString(args, "url", ""); u != "" {
		fields = append(fields, [2]string{"url", u})
	}
	if u := argString(args, "icon_url", ""); u != "" {
		fields = append(fields, [2]string{"icon_url", u})
	}

	body, status, err := curlPostForm(ctx, conn, "https://grove.io/api/notice/"+token+"/", fields, 30)
	if err != nil {
		return Fail("failed to send notification: " + err.Error()), nil
	}
	if status != 200 {
		return Fail(fmt.Sprintf("failed to send notification: HTTP %d", status)).
			WithExtra("status", status).
			WithExtra("body", strings.TrimSpace(body)), nil
	}
	return Ok("OK"), nil
}
