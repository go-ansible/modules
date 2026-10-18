package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleTelegram implements Ansible's `telegram` (community.general)
// module: calls a Telegram Bot API method, SendMessage by default.
//
// Telegram has no official CLI, so this posts to the same endpoint real
// telegram.py's own fetch_url call hits -- POST
// https://<api_host>/bot<token>/<api_method> with a JSON body -- through
// curl ON THE TARGET.
//
// Args: token (required); api_host (default "api.telegram.org");
// api_method (default "SendMessage"); api_args (a free-form dict passed
// through as the JSON body).
//
// Real seeds api_args with chat_id, parse_mode and text as explicit
// null when absent, and DELETES parse_mode when it is the string
// "plain". Both are reproduced: the body a bot receives is the same
// shape.
//
// check_mode is supported, as real declares (supports_check_mode=True):
// it reports changed=false and sends nothing. Note real reports
// changed=TRUE on a successful send, unlike grove and pushover -- this
// module is the one of the three that claims a change.
//
// # The token is in the URL path, and this port has no scrubber
//
// token is no_log=True in real's argument_spec, and the URL is built as
// .../bot<token>/<method>. curlDo sends the url on stdin through curl's
// own -K - config, so it never reaches argv.
//
// Real's failure message embeds that url:
//
//	f"Failed to send message, return status = {info['status']}\n
//	  url = {url}\napi_args = {api_args}"
//
// which looks like a leak and is NOT one: AnsibleModule's
// _return_formatted -- which exit_json and fail_json both run -- calls
// remove_values(kwargs, self.no_log_values), and that does SUBSTRING
// replacement, so the token inside the url comes out as "********"
// before anything prints it.
//
// ⛔ This port has no such machinery. It honours task-level
// `no_log: true`, which suppresses a whole result, but it has no
// equivalent of remove_values: nothing scrubs the VALUE of a no_log
// argument out of a message that happens to contain it. So the url is
// omitted here, not because real is careless but because real can
// afford a message this port cannot: reproducing that sentence would
// emit the bot token verbatim.
//
// The status and api_args are reported, which is everything in real's
// message that is not the credential.
func moduleTelegram(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	apiHost := argString(args, "api_host", "api.telegram.org")
	apiMethod := argString(args, "api_method", "SendMessage")

	apiArgs := map[string]any{}
	if raw, ok := args["api_args"].(map[string]any); ok {
		for k, v := range raw {
			apiArgs[k] = v
		}
	}
	// Real sets these three unconditionally, to null when absent.
	for _, k := range []string{"chat_id", "parse_mode", "text"} {
		if _, ok := apiArgs[k]; !ok {
			apiArgs[k] = nil
		}
	}
	// ...and removes parse_mode entirely when it is "plain".
	if s, _ := apiArgs["parse_mode"].(string); s == "plain" {
		delete(apiArgs, "parse_mode")
	}

	if InCheckMode(args) {
		// Real exits here with changed=False, before any request.
		return Result{}, nil
	}

	if res, ok := requireCurl(ctx, conn, "telegram", "Telegram"); !ok {
		return res, nil
	}

	bodyJSON, err := json.Marshal(apiArgs)
	if err != nil {
		return Fail("telegram: encoding api_args: " + err.Error()), nil
	}

	// url.PathEscape on the token matches real's own quote(token).
	endpoint := "https://" + apiHost + "/bot" + url.PathEscape(token) + "/" + apiMethod
	body, status, err := curlDo(ctx, conn, curlReq{
		Method:  "POST",
		URL:     endpoint,
		JSON:    string(bodyJSON),
		Headers: [][2]string{{"Content-Type", "application/json"}},
	})
	if err != nil {
		// Real's status -1 case: the request did not complete.
		return Fail("Failed to send message").WithExtra("error", err.Error()), nil
	}
	if status == 200 {
		return Changed(""), nil
	}

	// Real's wording, minus the url -- see the doc comment above.
	r := Fail(fmt.Sprintf("Failed to send message, return status = %d\napi_args = %v", status, apiArgs))
	var decoded map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(body)), &decoded) == nil {
		if desc, ok := decoded["description"].(string); ok {
			r = r.WithExtra("telegram_error", desc)
		}
	}
	return r.WithExtra("status", status), nil
}
