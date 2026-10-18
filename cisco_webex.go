package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCiscoWebex implements Ansible's `cisco_webex`
// (community.general) module: sends a message to a Webex Teams room or
// person.
//
// Webex has no official CLI, so this posts to the same endpoint real
// cisco_webex.py's own fetch_url call hits -- POST
// https://webexapis.com/v1/messages with a JSON body and an
// Authorization: Bearer header -- through curl ON THE TARGET.
//
// Args: recipient_type (roomId|toPersonEmail|toPersonId, required);
// recipient_id (required); msg (required); msg_type (text|markdown,
// default text, alias message_type); personal_token (required, alias
// token).
//
// # The payload keys are the VALUES of two arguments
//
// Real builds `{ansible["recipient_type"]: ansible["recipient_id"],
// ansible["msg_type"]: ansible["msg"]}` -- so `recipient_type:
// toPersonEmail` with `msg_type: markdown` sends
// {"toPersonEmail": ..., "markdown": ...}. That is reproduced, and it
// is why both arguments are restricted to real's own choices: an
// unchecked value would become a JSON key the API does not know.
//
// # Check mode makes a read-only call, as real does
//
// Real does not skip the request in check mode: it GETs
// https://webexapis.com/v1/people/me instead, which validates the token
// without sending anything, and reports "Authentication Successful."
// That is reproduced rather than simplified into a no-op, because the
// point of it is to tell a working token from a broken one.
//
// The bearer token and recipient_id are both no_log=True in real's
// argument_spec. curlDo sends headers, url and body on stdin, so
// neither reaches argv.
func moduleCiscoWebex(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	// Real's own aliases: message_type for msg_type, token for
	// personal_token.
	recipientType, err := requireString(args, "recipient_type")
	if err != nil {
		return Result{}, err
	}
	switch recipientType {
	case "roomId", "toPersonEmail", "toPersonId":
	default:
		return Fail("cisco_webex: value of recipient_type must be one of: roomId, toPersonEmail, toPersonId, got: " + recipientType), nil
	}
	recipientID, err := requireString(args, "recipient_id")
	if err != nil {
		return Result{}, err
	}
	msg, err := requireString(args, "msg")
	if err != nil {
		return Result{}, err
	}
	msgType := argString(args, "msg_type", argString(args, "message_type", "text"))
	switch msgType {
	case "text", "markdown":
	default:
		return Fail("cisco_webex: value of msg_type must be one of: text, markdown, got: " + msgType), nil
	}
	token := argString(args, "personal_token", argString(args, "token", ""))
	if token == "" {
		return Result{}, errArg("missing required argument: personal_token")
	}

	if res, ok := requireCurl(ctx, conn, "cisco_webex", "Cisco Webex"); !ok {
		return res, nil
	}

	headers := [][2]string{
		{"Authorization", "Bearer " + token},
		{"content-type", "application/json"},
	}

	if InCheckMode(args) {
		// Real's own read-only probe, not a skip.
		_, status, err := curlDo(ctx, conn, curlReq{
			Method: "GET", URL: "https://webexapis.com/v1/people/me", Headers: headers,
		})
		if err != nil {
			return Fail("cisco_webex: " + err.Error()), nil
		}
		if status != 200 {
			return Fail(fmt.Sprintf("HTTP %d", status)).
				WithExtra("status_code", status).
				WithExtra("message", fmt.Sprintf("HTTP %d", status)), nil
		}
		return Ok("Authentication Successful.").
			WithExtra("status_code", status).
			WithExtra("message", "Authentication Successful."), nil
	}

	payload, err := json.Marshal(map[string]any{recipientType: recipientID, msgType: msg})
	if err != nil {
		return Fail("cisco_webex: encoding payload: " + err.Error()), nil
	}
	body, status, err := curlDo(ctx, conn, curlReq{
		Method: "POST", URL: "https://webexapis.com/v1/messages",
		JSON: string(payload), Headers: headers,
	})
	if err != nil {
		return Fail("cisco_webex: " + err.Error()), nil
	}
	if status != 200 {
		return Fail(fmt.Sprintf("HTTP %d", status)).
			WithExtra("status_code", status).
			WithExtra("message", strings.TrimSpace(body)), nil
	}
	// Real reports fetch_url's own info["msg"] here, which is urllib's
	// status line and has no counterpart through curl; the status is
	// reported instead, and the body with it.
	return Ok(fmt.Sprintf("HTTP %d", status)).
		WithExtra("status_code", status).
		WithExtra("message", strings.TrimSpace(body)), nil
}
