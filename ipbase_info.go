package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// ipbaseURL and ipbaseUserAgent are real ipbase_info.py's own BASE_URL
// and USER_AGENT constants.
//
// The User-Agent is sent verbatim, which is the opposite of the choice
// made for Online.net in online.go, and the difference is the point.
// Online's real helper builds `ansible <version> Python <version>`: a
// claim about an interpreter that is not running here, so repeating it
// would put a false line in the operator's own request logs. This string
// names the MODULE, and this is that module — the service's rate limit
// and analytics are keyed to `ipbase_info`, and renaming the caller
// would change how the API treats it for no honest gain.
const (
	ipbaseURL       = "https://api.ipbase.com/v2/info"
	ipbaseUserAgent = "ansible-community.general.ipbase_info/0.1.0"
)

// moduleIpbaseInfo implements Ansible's `ipbase_info` (community.general)
// module: IP geolocation and related facts for an address, from
// ipbase.com's v2 API.
//
// Args: ip (the address to look up; omitted, the API uses the address
// the request came from); apikey (optional, for a higher rate limit);
// hostname (bool, default false — asks the API to resolve the address);
// language (ISO alpha-2, default "en").
//
// The response's own top-level keys become the result's, which is what
// real does: `module.exit_json(**ipbase.info())`. The API answers with
// `{"data": {...}}`, so the useful key is `data`, as real's RETURN block
// documents.
//
// # Two things real gets from AnsibleModule that this does not
//
// Real's splat hands the API control of the result's own status fields:
// a response carrying `changed` or `failed` at the top level would set
// them. Extra fields here are separate from the changed/failed pair, so
// this port cannot be told by a remote service that it made a change.
// That is a divergence, in the direction of the module keeping its own
// account of what it did.
//
// The second one is why the parse-failure message below is not real's
// word for word. Real interpolates the whole URL into it — query string,
// apikey and all — and is saved by AnsibleModule, which scrubs every
// `no_log` value out of a message before it is printed. This package has
// no such mechanism (the `no_log` mentions elsewhere in it are all
// documentation, not scrubbing), so repeating real's message literally
// would print the API key into the playbook's own output. The key's
// VALUE is replaced; everything else about the message is real's.
//
// The timeout is 10 seconds and is not an argument, matching real's own
// hardcoded `timeout=10`.
func moduleIpbaseInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := requireCurl(ctx, conn, "ipbase_info", "ipbase.com"); !ok {
		return res, nil
	}

	// Real builds `params` in this order and urlencodes it, so the query
	// string is the same one, parameter for parameter.
	var query [][2]string
	if ip := argString(args, "ip", ""); ip != "" {
		query = append(query, [2]string{"ip", ip})
	}
	apikey := argString(args, "apikey", "")
	if apikey != "" {
		query = append(query, [2]string{"apikey", apikey})
	}
	if argBool(args, "hostname", false) {
		// Real sends the integer 1, not "true".
		query = append(query, [2]string{"hostname", "1"})
	}
	if language := argString(args, "language", "en"); language != "" {
		query = append(query, [2]string{"language", language})
	}

	body, status, err := curlDo(ctx, conn, curlReq{
		Method: "GET",
		URL:    ipbaseURL,
		Query:  query,
		Headers: [][2]string{
			{"Accept", "application/json"},
			{"User-Agent", ipbaseUserAgent},
		},
		Timeout: 10,
	})
	if err != nil {
		return Fail("ipbase_info: " + err.Error()), nil
	}
	if status != 200 {
		return Fail(fmt.Sprintf(
			"The API request to ipbase.com returned an error status code %d", status)), nil
	}

	var fields map[string]any
	if jerr := json.Unmarshal([]byte(body), &fields); jerr != nil {
		return Fail(fmt.Sprintf("Failed to parse the ipbase.com response: %s %s",
			ipbaseSafeURL(query), strings.TrimSpace(body))), nil
	}
	return Result{Extra: fields}, nil
}

// ipbaseSafeURL rebuilds the request URL for a failure message with the
// apikey's value replaced.
//
// The parameter is kept rather than dropped: "which key was used" is
// part of what makes the message useful, and an operator reading
// `apikey=REDACTED` learns that one was sent, where a missing parameter
// would suggest none was.
func ipbaseSafeURL(query [][2]string) string {
	if len(query) == 0 {
		return ipbaseURL
	}
	parts := make([]string, 0, len(query))
	for _, q := range query {
		v := q[1]
		if q[0] == "apikey" {
			v = "REDACTED"
		}
		parts = append(parts, url.QueryEscape(q[0])+"="+url.QueryEscape(v))
	}
	return ipbaseURL + "?" + strings.Join(parts, "&")
}
