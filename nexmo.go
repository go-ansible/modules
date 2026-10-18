package modules

import (
	"context"
	"encoding/json"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleNexmo implements Ansible's `nexmo` (community.general) module:
// sends an SMS through Nexmo (now Vonage).
//
// Nexmo has no official CLI, so this calls the same endpoint real
// nexmo.py's own fetch_url call hits -- GET
// https://rest.nexmo.com/sms/json with the parameters in the QUERY
// STRING -- through curl ON THE TARGET. A GET, not a POST: real builds
// `url = f"{NEXMO_API}?{urlencode(msg)}"` and passes no data, which
// fetch_url sends as a GET.
//
// Args: api_key (required); api_secret (required); src (required);
// dest (required, one number or a list); msg (required). Wire parameter
// names are real's: api_key, api_secret, from, text, to.
//
// # Both credentials are in the query string
//
// api_key and api_secret are no_log=True in real's argument_spec and
// real puts them in the URL. curlDo sends the whole url, query string
// included, on stdin through curl's own -K - config, so neither can
// reach argv.
//
// # One deviation, and it is real's bug
//
// Real's send_msg calls module.exit_json INSIDE its `for number in
// dest` loop, so it returns after the FIRST destination and silently
// never sends to the rest. This port sends to every destination, which
// is plainly what the loop was written to do -- the same judgment
// sl_vm.go's own doc comment makes about real sl_vm.py's unassigned
// `instance` variable: implement the intent, do not reproduce a defect
// that makes the module not do its job.
//
// Real's own result shape is otherwise reproduced: a responses map
// keyed by destination number, failed set when any message failed, and
// changed FALSE. A per-message status other than "0" counts as a
// failure, as real checks. The summary message keeps real's wording,
// typo included ("One or messages failed to send"), because that is the
// string a playbook would see.
func moduleNexmo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	apiKey, err := requireString(args, "api_key")
	if err != nil {
		return Result{}, err
	}
	apiSecret, err := requireString(args, "api_secret")
	if err != nil {
		return Result{}, err
	}
	src, err := requireString(args, "src")
	if err != nil {
		return Result{}, err
	}
	msg, err := requireString(args, "msg")
	if err != nil {
		return Result{}, err
	}
	dests := argStringList(args, "dest")
	if len(dests) == 0 {
		return Result{}, errArg("missing required argument: dest")
	}

	if res, ok := requireCurl(ctx, conn, "nexmo", "Nexmo/Vonage"); !ok {
		return res, nil
	}

	responses := map[string]any{}
	var anyFailed bool
	for _, number := range dests {
		body, status, err := curlDo(ctx, conn, curlReq{
			Method: "GET",
			URL:    "https://rest.nexmo.com/sms/json",
			Query: [][2]string{
				{"api_key", apiKey},
				{"api_secret", apiSecret},
				{"from", src},
				{"text", msg},
				{"to", number},
			},
			Headers: [][2]string{{"Accept", "application/json"}},
		})
		if err != nil || status != 200 {
			anyFailed = true
			responses[number] = map[string]any{"failed": true}
			continue
		}
		var decoded map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(body)), &decoded) != nil {
			anyFailed = true
			responses[number] = map[string]any{"failed": true}
			continue
		}
		// Real walks responses[number]["messages"] and treats any
		// status other than "0" as a failure for that number.
		if failedMessage(decoded) {
			anyFailed = true
			decoded["failed"] = true
		}
		responses[number] = decoded
	}

	summary := ""
	if anyFailed {
		// Real's wording, typo and all.
		summary = "One or messages failed to send"
	}
	res := Result{Msg: summary, Failed: anyFailed}
	return res.WithExtra("responses", responses), nil
}

// failedMessage reports whether any entry of a Nexmo response's
// "messages" array carries a status other than "0". Nexmo sends the
// status as a string; a number is accepted too rather than assuming.
func failedMessage(decoded map[string]any) bool {
	list, _ := decoded["messages"].([]any)
	for _, m := range list {
		entry, _ := m.(map[string]any)
		switch s := entry["status"].(type) {
		case string:
			if s != "0" {
				return true
			}
		case float64:
			if s != 0 {
				return true
			}
		default:
			if entry["status"] != nil {
				return true
			}
		}
	}
	return false
}
