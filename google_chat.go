package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleGoogleChat implements Ansible's `google_chat`
// (community.general) module: posts a message to a Google Chat space
// through an incoming webhook.
//
// Google Chat has no official CLI, so this posts to the same endpoint
// real google_chat.py's own fetch_url call hits -- POST
// https://chat.googleapis.com/v1/spaces/<space>/messages with key and
// token as QUERY parameters and a JSON body -- through curl ON THE
// TARGET.
//
// Args: space (required); key (required); token (required); text
// (required); thread_key; create_new_thread (default true).
//
// # Both credentials are query parameters
//
// key and token are no_log=True in real's argument_spec, and real puts
// them in the query string. curlDo builds the query and sends the whole
// url on stdin through curl's own -K - config, so neither reaches argv.
//
// # Threading, and real's two reply options
//
// With thread_key set, real adds thread.threadKey to the body AND a
// messageReplyOption query parameter:
// REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD when create_new_thread is true
// (its default), REPLY_MESSAGE_OR_FAIL when false. Both are reproduced
// -- the difference decides whether a reply to a vanished thread starts
// a new one or errors.
//
// # Check mode claims a change without making one
//
// Real's check-mode branch is `module.exit_json(changed=True)` -- it
// reports changed=TRUE and sends nothing, where most modules report
// changed=False. Reproduced as real has it: a playbook's --check run
// sees the same answer from both implementations.
//
// On success real reports changed=True plus name and thread_name when
// the response carries them, and on a non-200 it fails with
// "Failed to send message to Google Chat (HTTP <status>): <body>".
// Both are reproduced.
func moduleGoogleChat(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	space, err := requireString(args, "space")
	if err != nil {
		return Result{}, err
	}
	key, err := requireString(args, "key")
	if err != nil {
		return Result{}, err
	}
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	text, err := requireString(args, "text")
	if err != nil {
		return Result{}, err
	}
	threadKey := argString(args, "thread_key", "")
	createNewThread := argBool(args, "create_new_thread", true)

	if InCheckMode(args) {
		// Real reports a change here and sends nothing.
		return Changed(""), nil
	}

	if res, ok := requireCurl(ctx, conn, "google_chat", "Google Chat"); !ok {
		return res, nil
	}

	payload := map[string]any{"text": text}
	query := [][2]string{{"key", key}, {"token", token}}
	if threadKey != "" {
		payload["thread"] = map[string]any{"threadKey": threadKey}
		option := "REPLY_MESSAGE_OR_FAIL"
		if createNewThread {
			option = "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD"
		}
		query = append(query, [2]string{"messageReplyOption", option})
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return Fail("google_chat: encoding payload: " + err.Error()), nil
	}

	body, status, err := curlDo(ctx, conn, curlReq{
		Method: "POST",
		URL:    "https://chat.googleapis.com/v1/spaces/" + space + "/messages",
		Query:  query,
		JSON:   string(encoded),
		Headers: [][2]string{
			{"Content-Type", "application/json; charset=UTF-8"},
			{"Accept", "application/json"},
		},
	})
	if err != nil {
		return Fail("Failed to send message to Google Chat: " + err.Error()), nil
	}
	if status != 200 {
		return Fail(fmt.Sprintf("Failed to send message to Google Chat (HTTP %d): %s",
			status, strings.TrimSpace(body))), nil
	}

	res := Changed("")
	var decoded map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(body)), &decoded) == nil {
		if name, ok := decoded["name"].(string); ok {
			res = res.WithExtra("name", name)
		}
		if thread, ok := decoded["thread"].(map[string]any); ok {
			if name, ok := thread["name"].(string); ok {
				res = res.WithExtra("thread_name", name)
			}
		}
	}
	return res, nil
}
