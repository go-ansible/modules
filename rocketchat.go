package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleRocketchat implements Ansible's `rocketchat`
// (community.general) module: posts to a Rocket.Chat incoming webhook.
//
// Rocket.Chat has no official CLI, so this posts to the same endpoint
// real rocketchat.py's own fetch_url call hits --
// <protocol>://<domain>/hooks/<token> -- through curl ON THE TARGET.
//
// Args: domain (required); token (required); protocol (http|https,
// default https); msg; channel; username (default "Ansible"); icon_url
// (default https://docs.ansible.com/favicon/favicon.ico); icon_emoji;
// link_names (0|1, default 1); color (normal|good|warning|danger,
// default normal); attachments; is_pre740 (default false).
//
// # color decides whether msg is text or an attachment
//
// With color "normal" the message is {"text": msg}. With any other
// colour real wraps it instead: {"attachments": [{"text": msg,
// "color": color}]} -- the text field is then absent entirely. That is
// real's own branch, and it is why color is not merely cosmetic.
//
// # is_pre740 changes the ENCODING, not just a header
//
// Rocket.Chat before 7.4.0 wants the JSON wrapped in a form field:
// real sends the body as `payload=<json>` and sends NO Content-Type at
// all. From 7.4.0 it posts the JSON directly with
// Content-Type: application/json. Both are reproduced; sending the
// modern shape to an old server, or the reverse, is a silent
// no-delivery rather than an error.
//
// # icon_emoji and icon_url are exclusive, real's way round
//
// Real sets icon_emoji when given and icon_url OTHERWISE -- so passing
// an emoji drops the url rather than sending both. An attachment with
// no "fallback" gets its own text copied into one, as real does.
//
// A token with no "/" in it is refused before any request, with real's
// own wording: those webhook tokens are two path segments.
//
// Real reports msg="OK" with changed unset, and fails with
// "failed to send message, return status=<status>". There is no check
// mode: real does not declare supports_check_mode.
func moduleRocketchat(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	domain, err := requireString(args, "domain")
	if err != nil {
		return Result{}, err
	}
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	if strings.Count(token, "/") < 1 {
		// Real's own check and wording.
		return Fail("Invalid Token specified, provide a valid token"), nil
	}
	protocol := argString(args, "protocol", "https")
	switch protocol {
	case "http", "https":
	default:
		return Fail("rocketchat: value of protocol must be one of: http, https, got: " + protocol), nil
	}
	color := argString(args, "color", "normal")
	switch color {
	case "normal", "good", "warning", "danger":
	default:
		return Fail("rocketchat: value of color must be one of: normal, good, warning, danger, got: " + color), nil
	}
	isPre740 := argBool(args, "is_pre740", false)

	payload := map[string]any{}
	// Real tests `text is not None`, so msg: "" is still a message --
	// presence, not emptiness, decides.
	if _, given := args["msg"]; given {
		text := argString(args, "msg", "")
		if color == "normal" {
			payload["text"] = text
		} else {
			payload["attachments"] = []any{map[string]any{"text": text, "color": color}}
		}
	}
	if channel := argString(args, "channel", ""); channel != "" {
		// Real prefixes a bare name with #, and leaves # or @ alone.
		if channel[0] == '#' || channel[0] == '@' {
			payload["channel"] = channel
		} else {
			payload["channel"] = "#" + channel
		}
	}
	payload["username"] = argString(args, "username", "Ansible")
	// Real's own either/or: an emoji REPLACES the url.
	if emoji := argString(args, "icon_emoji", ""); emoji != "" {
		payload["icon_emoji"] = emoji
	} else {
		payload["icon_url"] = argString(args, "icon_url", "https://docs.ansible.com/favicon/favicon.ico")
	}
	linkNames := argInt(args, "link_names", 1)
	if linkNames != 0 && linkNames != 1 {
		return Fail(fmt.Sprintf("rocketchat: value of link_names must be one of: 0, 1, got: %d", linkNames)), nil
	}
	payload["link_names"] = linkNames

	if raw, ok := args["attachments"].([]any); ok {
		// Real creates the key before the loop, so attachments: [] sends
		// "attachments": [] rather than omitting it.
		existing, _ := payload["attachments"].([]any)
		if existing == nil {
			existing = []any{}
		}
		for _, a := range raw {
			att, _ := a.(map[string]any)
			if att == nil {
				continue
			}
			if _, has := att["fallback"]; !has {
				// Real copies text into fallback when absent.
				att["fallback"] = att["text"]
			}
			existing = append(existing, att)
		}
		payload["attachments"] = existing
	}

	if res, ok := requireCurl(ctx, conn, "rocketchat", "Rocket.Chat"); !ok {
		return res, nil
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return Fail("rocketchat: encoding payload: " + err.Error()), nil
	}

	req := curlReq{
		Method: "POST",
		URL:    protocol + "://" + domain + "/hooks/" + token,
		JSON:   string(encoded),
	}
	if isPre740 {
		// Real sends the JSON RAW behind "payload=" -- not url-encoded --
		// and sends no Content-Type at all. Both are reproduced
		// literally: a percent-encoded value would be the more correct
		// form body, but it is not what the old servers were given, and
		// the empty header value is how curl is told to withhold the
		// Content-Type it would otherwise add by itself.
		req.JSON = "payload=" + string(encoded)
		req.Headers = [][2]string{{"Content-Type", ""}}
	} else {
		req.Headers = [][2]string{{"Content-type", "application/json"}}
	}

	_, status, err := curlDo(ctx, conn, req)
	if err != nil {
		return Fail("failed to send message: " + err.Error()), nil
	}
	if status != 200 {
		return Fail(fmt.Sprintf("failed to send message, return status=%d", status)), nil
	}
	return Ok("OK"), nil
}
