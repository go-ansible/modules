package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpifyFacts implements Ansible's `ipify_facts` (community.general)
// module: the public IP address of the gateway the target sits behind.
//
// Args: api_url (default https://api.ipify.org/); timeout (seconds,
// default 10); validate_certs (default true).
//
// It sets one fact, `ipify_public_ip`, and reports changed=false.
//
// # `?format=json` is concatenated, not merged
//
// Real builds its URL as `f"{self.api_url}?format=json"`. With the
// default that gives `https://api.ipify.org/?format=json`; with an
// api_url that already carries a query it gives a URL with two `?` in
// it, which the service will not answer. That is reproduced rather than
// repaired: a port that quietly merged the query would answer where real
// fails, and a playbook built against real's behaviour would then take a
// different path here.
//
// # Where real reports a timeout, and where it does not
//
// Real's only guard is `if not response`, and the message it prints
// blames a timeout. Reading `fetch_url` rather than guessing at it
// (ansible-core 2.21.4, module_utils/urls.py): on `urllib.error.HTTPError`
// it sets `r = e` — the exception itself is the response — after having
// already consumed the body with `e.read()`. So for any HTTP error status
// `response` is TRUTHY and the guard does not fire; real goes on to
// `json.loads(response.read())` on an empty string and dies with a
// ValueError traceback. The friendly message is reached only when `r`
// stays None: a DNS failure, a refused connection, a genuine timeout.
//
// This port keeps real's sentence for exactly that case, and for an HTTP
// error status says what happened instead of raising. Both lose the run;
// only one of them tells the operator which status it got.
func moduleIpifyFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if res, ok := requireCurl(ctx, conn, "ipify_facts", "ipify.org"); !ok {
		return res, nil
	}

	apiURL := argString(args, "api_url", "https://api.ipify.org/")
	timeout := argInt(args, "timeout", 10)

	body, status, err := curlDo(ctx, conn, curlReq{
		Method: "GET",
		// Real's own concatenation, kept whole -- see the doc comment.
		URL:      apiURL + "?format=json",
		Timeout:  timeout,
		Insecure: !argBool(args, "validate_certs", true),
	})
	if err != nil {
		// The `r is None` case: nothing came back at all. Real's wording,
		// including its own parenthesised "(timeout)", which is a guess
		// on real's part as much as on this port's -- a refused
		// connection reaches it too.
		return Fail(fmt.Sprintf("No valid or no response from url %s within %d seconds (timeout)",
			apiURL, timeout)), nil
	}
	if status < 200 || status > 299 {
		return Fail(fmt.Sprintf("ipify_facts: %s answered with status %d", apiURL, status)), nil
	}

	var data struct {
		IP *string `json:"ip"`
	}
	if jerr := json.Unmarshal([]byte(body), &data); jerr != nil {
		return Fail(fmt.Sprintf("ipify_facts: the answer from %s is not JSON: %s",
			apiURL, strings.TrimSpace(body))), nil
	}

	// `data.get("ip")` on a body without one is None, and the fact is set
	// to it rather than left out. A pointer keeps that distinction: a
	// missing `ip` is a null fact, not an empty string.
	var ip any
	if data.IP != nil {
		ip = *data.IP
	}
	return Result{Facts: map[string]any{"ipify_public_ip": ip}}, nil
}
