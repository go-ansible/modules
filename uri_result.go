package modules

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// uriHeaderKey turns an HTTP response header name into the result key
// real uses: lowercased, with hyphens as underscores. Real builds its
// result by walking the response's headers and doing
// `uresp[ukey] = value`, so Content-Type becomes content_type and
// Last-Modified becomes last_modified.
func uriHeaderKey(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_")
}

// uriResponse is one parsed HTTP response: the last header block (so a
// followed redirect reports the final hop's headers, as real does), the
// status line's reason phrase, and the body.
type uriResponse struct {
	status  int
	reason  string
	headers map[string]string
	body    string
}

// parseURIResponse splits `curl -i` output into headers and body. A
// followed redirect produces SEVERAL header blocks; real reports the
// final response's headers, so the last block wins.
func parseURIResponse(out string) uriResponse {
	r := uriResponse{headers: map[string]string{}}
	// Normalise CRLF so the blank-line split works either way.
	out = strings.ReplaceAll(out, "\r\n", "\n")

	rest := out
	for {
		// A header block starts with a status line and ends at the
		// first blank line.
		if !strings.HasPrefix(rest, "HTTP/") {
			break
		}
		cut := strings.Index(rest, "\n\n")
		var block string
		if cut < 0 {
			block, rest = rest, ""
		} else {
			block, rest = rest[:cut], rest[cut+2:]
		}
		lines := strings.Split(block, "\n")
		r.status, r.reason = parseStatusLine(lines[0])
		r.headers = map[string]string{}
		for _, l := range lines[1:] {
			name, value, ok := strings.Cut(l, ":")
			if !ok {
				continue
			}
			r.headers[uriHeaderKey(name)] = strings.TrimSpace(value)
		}
		if rest == "" || !strings.HasPrefix(rest, "HTTP/") {
			break
		}
	}
	r.body = rest
	return r
}

// parseStatusLine reads "HTTP/1.0 200 OK" into 200 and "OK". The reason
// phrase is what real puts in msg -- measured: a 200 on a 12-byte body
// gives msg "OK (12 bytes)", not "OK (200)".
func parseStatusLine(line string) (int, string) {
	parts := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(parts) < 2 {
		return 0, ""
	}
	code, _ := strconv.Atoi(parts[1])
	reason := ""
	if len(parts) == 3 {
		reason = parts[2]
	}
	return code, reason
}

// uriResultKeys adds every key real reports for a uri response.
//
// Measured against ansible-core 2.21.4 for a 200 on a 12-byte JSON
// body, with no return_content:
//
//	changed,content_length,content_type,cookies,cookies_string,date,
//	elapsed,failed,json,last_modified,msg,redirected,server,status,url
//
// -- fifteen keys, where this module reported six. The variable part is
// the response's own headers: real lowercases each one and makes it a
// key, so content_length/content_type/date/last_modified/server here
// are the server's headers rather than a fixed list this port could
// hardcode.
//
// `content` appears only with return_content: true. `json` appears only
// when the body parses as JSON. `msg` is the status line's REASON
// phrase plus the body size -- "OK (12 bytes)".
func uriResultKeys(r Result, resp uriResponse, url, effectiveURL string, elapsed float64, returnContent bool) Result {
	for k, v := range resp.headers {
		r = r.WithExtra(k, v)
	}
	r = r.WithExtra("status", resp.status).
		WithExtra("url", url).
		WithExtra("elapsed", int(elapsed)).
		WithExtra("redirected", effectiveURL != "" && effectiveURL != url)

	// Cookies come from Set-Cookie. Real reports an empty dict and an
	// empty string when there are none, not an absent key.
	cookies := map[string]any{}
	var pairs []string
	if sc := resp.headers["set_cookie"]; sc != "" {
		for _, c := range strings.Split(sc, ",") {
			nv := strings.TrimSpace(strings.SplitN(c, ";", 2)[0])
			if name, value, ok := strings.Cut(nv, "="); ok {
				cookies[strings.TrimSpace(name)] = strings.TrimSpace(value)
				pairs = append(pairs, strings.TrimSpace(name)+"="+strings.TrimSpace(value))
			}
		}
	}
	r = r.WithExtra("cookies", cookies).
		WithExtra("cookies_string", strings.Join(pairs, "; "))

	if returnContent {
		r = r.WithExtra("content", resp.body)
	}
	// json only when the body actually parses as one, which is real's
	// rule -- it tries and skips the key on failure rather than
	// reporting null.
	var parsed any
	if err := json.Unmarshal([]byte(resp.body), &parsed); err == nil {
		r = r.WithExtra("json", parsed)
	}
	return r
}

// uriMsg is real's own message: the reason phrase and the body size.
func uriMsg(resp uriResponse) string {
	return fmt.Sprintf("%s (%d bytes)", resp.reason, len(resp.body))
}
