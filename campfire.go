package modules

import (
	"context"
	"fmt"
	"html"
	"slices"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCampfire implements Ansible's `campfire` (community.general)
// module: sends a message, and optionally a sound, to a Campfire room.
//
// Campfire has no official CLI, so this posts to the same endpoint real
// campfire.py's own fetch_url calls hit -- POST
// https://<subscription>.campfirenow.com/room/<room>/speak.xml with an
// XML body -- through curl ON THE TARGET.
//
// Args: subscription (required); token (required); room (required); msg
// (required); notify (one of real's own sound names).
//
// # Two requests when notify is given, in real's order
//
// Real sends the SOUND first, as its own message with
// <type>SoundMessage</type>, and then the text. Two separate posts, not
// one combined body -- and the sound going first is what makes it play
// before the message appears. Reproduced, including that a failed sound
// aborts before the text is sent.
//
// # The token is HTTP Basic, not a header or a field
//
// Real sets url_username=token and url_password="X", so the token is
// the Basic auth user. curlDo sends it through curl's own `-K -` stdin
// config as a `user = "..."` directive, the same way infinityCurl does;
// -u would put it in argv.
//
// Success is 200 OR 201, which is what real accepts; anything else
// fails with real's own wording, the offending message quoted in it.
// Real reports changed=True with room, msg and notify echoed back.
//
// Both values are HTML-escaped before going into the XML, as real does
// with html_escape -- without it a msg containing & or < produces a
// body the API rejects, or worse, one it misparses.
func moduleCampfire(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	subscription, err := requireString(args, "subscription")
	if err != nil {
		return Result{}, err
	}
	token, err := requireString(args, "token")
	if err != nil {
		return Result{}, err
	}
	room, err := requireString(args, "room")
	if err != nil {
		return Result{}, err
	}
	msg, err := requireString(args, "msg")
	if err != nil {
		return Result{}, err
	}
	notify := argString(args, "notify", "")
	if notify != "" && !slices.Contains(campfireSounds, notify) {
		// Real's framework refuses an unlisted sound before the module
		// body runs; these are Campfire's own sound names, not free text.
		return Fail("campfire: value of notify must be one of: " + strings.Join(campfireSounds, ", ") + ", got: " + notify), nil
	}

	if res, ok := requireCurl(ctx, conn, "campfire", "Campfire"); !ok {
		return res, nil
	}

	target := "https://" + subscription + ".campfirenow.com/room/" + room + "/speak.xml"
	post := func(body string) (int, error) {
		_, status, err := curlDo(ctx, conn, curlReq{
			Method: "POST", URL: target, JSON: body,
			BasicAuth: token + ":X",
			Headers: [][2]string{
				{"Content-Type", "application/xml"},
				{"User-agent", "Ansible/1.2"},
			},
		})
		return status, err
	}

	// Real sends the sound FIRST, and gives up if it fails.
	if notify != "" {
		status, err := post("<message><type>SoundMessage</type><body>" + html.EscapeString(notify) + "</body></message>")
		if err != nil {
			return Fail(fmt.Sprintf("unable to send msg: '%s', campfire api returned error code: '%s'", notify, err)), nil
		}
		if status != 200 && status != 201 {
			return Fail(fmt.Sprintf("unable to send msg: '%s', campfire api returned error code: '%d'", notify, status)), nil
		}
	}

	status, err := post("<message><body>" + html.EscapeString(msg) + "</body></message>")
	if err != nil {
		return Fail(fmt.Sprintf("unable to send msg: '%s', campfire api returned error code: '%s'", msg, err)), nil
	}
	if status != 200 && status != 201 {
		return Fail(fmt.Sprintf("unable to send msg: '%s', campfire api returned error code: '%d'", msg, status)), nil
	}

	result := Changed("").
		WithExtra("room", room).
		WithExtra("msg", msg)
	if notify == "" {
		// Real emits notify=None when it was not given, not "".
		return result.WithExtra("notify", nil), nil
	}
	return result.WithExtra("notify", notify), nil
}

// campfireSounds is real's own `notify` choices list, in real's own
// order -- Campfire's built-in sound names. Real rejects anything else
// in the argument spec, before the module body runs.
var campfireSounds = []string{
	"56k", "bell", "bezos", "bueller", "clowntown", "cottoneyejoe",
	"crickets", "dadgummit", "dangerzone", "danielsan", "deeper",
	"drama", "greatjob", "greyjoy", "guarantee", "heygirl", "horn",
	"horror", "inconceivable", "live", "loggins", "makeitso", "noooo",
	"nyan", "ohmy", "ohyeah", "pushit", "rimshot", "rollout", "rumble",
	"sax", "secret", "sexyback", "story", "tada", "tmyk", "trololo",
	"trombone", "unix", "vuvuzela", "what", "whoomp", "yeah", "yodel",
}
