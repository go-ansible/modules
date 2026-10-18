package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePushover implements Ansible's `pushover` (community.general)
// module: sends a notification through pushover.net.
//
// Pushover has no official CLI, so there is nothing to shell out to the
// way this package does for `gh`/`glab`/`scw`. It posts to the same
// endpoint real pushover.py's own fetch_url call hits --
// POST https://api.pushover.net/1/messages.json, form-encoded -- via
// `curl` ON THE TARGET through curlPostForm. The request therefore
// originates where real's own module runs, which is the target, not the
// controller.
//
// Args: msg (required); app_token (required); user_key (required); pri
// (-2|-1|0|1|2, default "0"); title; device. Field names on the wire are
// real's: user, token, priority, message, title, device.
//
// # Both credentials stay off the command line
//
// app_token and user_key are no_log=True in real's own argument_spec,
// and here they are form FIELDS -- so the body cannot go in argv. It
// does not: curlPostForm sends the url and the whole body on stdin
// through curl's own `-K -` config. pushover_test.go asserts that
// neither secret appears in any command string.
//
// # Two faithfulness notes
//
// Real's success message is "message sent successfully: " followed by
// the raw response body, and changed is FALSE -- sending a notification
// reports no change, which is what a playbook's changed_when would see.
// Both are reproduced exactly.
//
// Real's failure message is the fixed sentence "Unable to send msg via
// pushover" and nothing else: its own `except Exception` discards the
// cause, so a non-200 and an unreachable host are indistinguishable to
// a playbook. The sentence is reproduced byte for byte, since a
// playbook may match on it, but the status and body are attached as
// Extra rather than thrown away -- real loses information here and
// there is no reason for this port to lose it too.
func modulePushover(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	msg, err := requireString(args, "msg")
	if err != nil {
		return Result{}, err
	}
	appToken, err := requireString(args, "app_token")
	if err != nil {
		return Result{}, err
	}
	userKey, err := requireString(args, "user_key")
	if err != nil {
		return Result{}, err
	}
	pri := argString(args, "pri", "0")
	switch pri {
	case "-2", "-1", "0", "1", "2":
	default:
		return Fail(fmt.Sprintf("pushover: value of pri must be one of: -2, -1, 0, 1, 2, got: %s", pri)), nil
	}

	if res, ok := requireCurl(ctx, conn, "pushover", "pushover.net"); !ok {
		return res, nil
	}

	// Real's own option order in Pushover.run: user, token, priority,
	// message, then title and device when given.
	fields := [][2]string{
		{"user", userKey},
		{"token", appToken},
		{"priority", pri},
		{"message", msg},
	}
	if title := argString(args, "title", ""); title != "" {
		fields = append(fields, [2]string{"title", title})
	}
	if device := argString(args, "device", ""); device != "" {
		fields = append(fields, [2]string{"device", device})
	}

	body, status, err := curlPostForm(ctx, conn, "https://api.pushover.net/1/messages.json", fields, 30)
	if err != nil || status != 200 {
		// Real's wording, which carries no cause at all; the cause goes
		// in Extra instead of being discarded.
		r := Fail("Unable to send msg via pushover")
		if err != nil {
			return r.WithExtra("error", err.Error()), nil
		}
		return r.WithExtra("status", status).WithExtra("body", strings.TrimSpace(body)), nil
	}

	return Ok("message sent successfully: " + body), nil
}
