package modules

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleURI implements (a subset of) Ansible's `uri` module: issues an
// HTTP(S) request and checks the response status code.
//
// Real uri runs on the target already (Python was copied there before
// the module ran); this port composes the request as a remote curl
// invocation via conn.Exec instead of doing the HTTP request from the
// control node — the control node may not share the target's network
// reachability (an internal service, a proxy only the target can see),
// and issuing the request from the wrong place would silently change
// what real uri observes.
//
// Args: url (string, required); method (string, default "GET");
// status_code (int or []int, default [200]); body (string, optional);
// headers (map[string]any, optional).
//
// Simplifications vs real uri: no digest/basic/WSSE auth, no
// body_format encoding (body is sent as-is via curl's -d), no
// return_content/dest/redirect/timeout/SSL-tuning knobs. Status and
// body are both captured from a single curl invocation using a
// trailing marker line ("\nHTTPSTATUS:<code>") rather than two separate
// requests, so a non-idempotent method (POST, etc.) is only ever sent
// once.
func moduleURI(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	url, err := requireString(args, "url")
	if err != nil {
		return Result{}, err
	}
	method := strings.ToUpper(argString(args, "method", "GET"))
	wantCodes, err := uriStatusCodes(args)
	if err != nil {
		return Result{}, err
	}
	body := argString(args, "body", "")
	headers, _ := args["headers"].(map[string]any)

	res, err := runStatus(ctx, conn, uriCmd(method, url, body, headers))
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail(fmt.Sprintf("uri: request to %s failed: %s", url, strings.TrimSpace(res.Stderr))), nil
	}

	respBody, status, err := parseCurlStatus(res.Stdout)
	if err != nil {
		return Result{}, fmt.Errorf("uri: %w", err)
	}

	ok := false
	for _, c := range wantCodes {
		if c == status {
			ok = true
			break
		}
	}
	if !ok {
		return Fail(fmt.Sprintf("Status code was %d and not %v: %s", status, wantCodes, respBody)).
			WithExtra("status", status).WithExtra("content", respBody), nil
	}

	r := Ok(fmt.Sprintf("OK (%d)", status))
	if method != "GET" && method != "HEAD" {
		// Matches real uri's own rule of thumb: a GET/HEAD is assumed
		// read-only and never reported changed; anything else (POST,
		// PUT, DELETE, ...) is assumed to have side effects.
		r = Changed(fmt.Sprintf("OK (%d)", status))
	}
	r = r.WithExtra("status", status)
	r = r.WithExtra("content", respBody)
	r = r.WithExtra("url", url)
	return r, nil
}

// uriCmd builds the curl invocation for moduleURI, separated out so its
// exact shape can be asserted directly in tests. Headers are emitted in
// sorted key order for a deterministic command line.
func uriCmd(method, url, body string, headers map[string]any) string {
	var b strings.Builder
	b.WriteString("curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}") + " -X " + shellQuote(method))
	if len(headers) > 0 {
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(" -H " + shellQuote(fmt.Sprintf("%s: %s", k, fmt.Sprint(headers[k]))))
		}
	}
	if body != "" {
		b.WriteString(" -d " + shellQuote(body))
	}
	b.WriteString(" " + shellQuote(url))
	return b.String()
}

// parseCurlStatus splits a curl response captured with
// -w '\nHTTPSTATUS:%{http_code}' into its body and numeric status code.
func parseCurlStatus(out string) (body string, status int, err error) {
	marker := "\nHTTPSTATUS:"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		return "", 0, fmt.Errorf("could not find HTTP status marker in response %q", out)
	}
	body = out[:idx]
	statusStr := strings.TrimSpace(out[idx+len(marker):])
	status, err = strconv.Atoi(statusStr)
	if err != nil {
		return "", 0, fmt.Errorf("parsing HTTP status %q: %w", statusStr, err)
	}
	return body, status, nil
}

// uriStatusCodes normalizes the status_code argument (an int or a list
// of ints/strings) into a []int, defaulting to [200].
func uriStatusCodes(args map[string]any) ([]int, error) {
	v, ok := args["status_code"]
	if !ok {
		return []int{200}, nil
	}
	toInt := func(item any) (int, error) {
		switch n := item.(type) {
		case int:
			return n, nil
		case int64:
			return int(n), nil
		case float64:
			return int(n), nil
		case string:
			parsed, err := strconv.Atoi(n)
			if err != nil {
				return 0, errArg("uri: status_code entry %q is not a number", n)
			}
			return parsed, nil
		default:
			return 0, errArg("uri: status_code entry has unsupported type %T", item)
		}
	}
	if list, ok := v.([]any); ok {
		out := make([]int, 0, len(list))
		for _, item := range list {
			n, err := toInt(item)
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		return out, nil
	}
	n, err := toInt(v)
	if err != nil {
		return nil, err
	}
	return []int{n}, nil
}
