package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleDiscord implements Ansible's `discord` (community.general)
// module: posts a message to a Discord channel through a webhook.
//
// Discord has no official CLI, so this posts to the same endpoint real
// discord.py's own fetch_url call hits -- POST
// https://discord.com/api/webhooks/<webhook_id>/<webhook_token> with a
// JSON body -- through curl ON THE TARGET.
//
// Args: webhook_id (required); webhook_token (required); content;
// username; avatar_url; tts (default false); embeds. At least one of
// content and embeds is required, as real's own required_one_of says.
//
// # Success is 204, not 200
//
// Discord's execute-webhook endpoint answers 204 No Content, and real
// checks for exactly that; a 200 would be a failure there. Reproduced.
// Real's check-mode GET of the same URL does expect 200, which is the
// webhook-lookup endpoint answering -- the two are different calls with
// different success codes, which is easy to collapse by accident.
//
// # The webhook token is in the URL path
//
// webhook_token is no_log=True in real's argument_spec and sits in the
// path, so the url itself is a credential. curlDo sends it on stdin
// through curl's own -K - config; it never reaches argv.
//
// Real sends all five payload keys whether or not they were given,
// null when absent, and that shape is reproduced: a webhook receiving
// {"content": "x", "username": null, ...} is what real produces.
func moduleDiscord(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	webhookID, err := requireString(args, "webhook_id")
	if err != nil {
		return Result{}, err
	}
	webhookToken, err := requireString(args, "webhook_token")
	if err != nil {
		return Result{}, err
	}
	content := argString(args, "content", "")
	_, hasEmbeds := args["embeds"]
	if content == "" && !hasEmbeds {
		// Real's own required_one_of=[["content", "embeds"]].
		return Fail("discord: one of the following is required: content, embeds"), nil
	}

	if res, ok := requireCurl(ctx, conn, "discord", "Discord"); !ok {
		return res, nil
	}

	url := "https://discord.com/api/webhooks/" + webhookID + "/" + webhookToken
	headers := [][2]string{{"content-type", "application/json"}}

	if InCheckMode(args) {
		// Real GETs the webhook to validate it, and expects 200 here.
		body, status, err := curlDo(ctx, conn, curlReq{Method: "GET", URL: url, Headers: headers})
		if err != nil {
			return Fail("discord: " + err.Error()), nil
		}
		r := Result{Msg: fmt.Sprintf("HTTP %d", status)}
		if status != 200 {
			r.Failed = true
		}
		return r.WithExtra("http_code", status).WithExtra("response", strings.TrimSpace(body)), nil
	}

	payload := map[string]any{
		"content":    nilIfEmpty(content),
		"username":   nilIfEmpty(argString(args, "username", "")),
		"avatar_url": nilIfEmpty(argString(args, "avatar_url", "")),
		"tts":        argBool(args, "tts", false),
		"embeds":     args["embeds"],
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Fail("discord: encoding payload: " + err.Error()), nil
	}

	body, status, err := curlDo(ctx, conn, curlReq{
		Method: "POST", URL: url, JSON: string(encoded), Headers: headers,
	})
	if err != nil {
		return Fail("discord: " + err.Error()), nil
	}
	// 204 No Content is the success here, which is what real checks.
	if status != 204 {
		return Fail(fmt.Sprintf("HTTP %d", status)).
			WithExtra("http_code", status).
			WithExtra("response", strings.TrimSpace(body)), nil
	}
	return Changed(fmt.Sprintf("HTTP %d", status)).WithExtra("http_code", status), nil
}
