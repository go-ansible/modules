package modules

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// curlPostForm POSTs application/x-www-form-urlencoded fields through
// `curl` ON THE TARGET, the way this package reaches every HTTP-only
// service: a module acts from the host it was dispatched to, and
// conn.Exec is the only thing that can. Real Ansible's own notification
// modules run on the target too and call fetch_url from there, so the
// request originates in the same place.
//
// This is the shape infinityCurl and icinga2CurlRequest already use,
// factored out for the services that have no vendor quirks to carry --
// a plain form POST to one endpoint. Those two keep their own wrappers
// for now (infinity hardcodes -k to match its reference, icinga2 carries
// an API-user header), and are candidates to migrate onto this.
//
// # Nothing that could be a credential reaches argv
//
// The url AND the whole body travel on STDIN, through curl's own
// `-K -` config -- a real curl feature, where any long option may
// appear as `name = "value"` on its own line. That matters twice over
// here: a form field can be a token (pushover's app_token), and for a
// webhook service the URL ITSELF is the credential. Only flags with no
// secret in them are left on the command line.
//
// Fields are an ordered slice, not a map: a map's iteration order would
// make the body -- and therefore the stdin a test asserts on --
// different run to run.
// curlReq describes one request. Every field that could carry a
// credential -- URL, Query, Form, JSON -- is sent on STDIN, never argv.
type curlReq struct {
	Method  string      // "GET" or "POST"
	URL     string      // for a webhook or path-token service this IS the credential
	Query   [][2]string // appended as a query string; nexmo puts its api_secret here
	Form    [][2]string // application/x-www-form-urlencoded body
	JSON    string      // raw JSON body, when the service wants one
	Headers [][2]string // extra request headers
	Timeout int         // seconds; 30 when zero
}

// curlDo runs one request through curl ON THE TARGET and returns the
// body and HTTP status.
//
// Ordered slices rather than maps throughout: a map's iteration order
// would make the query string, the body, and therefore the stdin a test
// asserts on, differ from run to run.
func curlDo(ctx context.Context, conn remoteexec.Connection, req curlReq) (body string, status int, err error) {
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	if req.Method == "" {
		req.Method = "GET"
	}

	endpoint := req.URL
	if len(req.Query) > 0 {
		parts := make([]string, 0, len(req.Query))
		for _, q := range req.Query {
			parts = append(parts, url.QueryEscape(q[0])+"="+url.QueryEscape(q[1]))
		}
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		endpoint += sep + strings.Join(parts, "&")
	}

	// -s so the progress meter stays out of stdout; -w appends the
	// status in the shape parseCurlStatus reads, the marker uri.go and
	// infinity.go already use.
	cmd := "curl -s -K - --max-time " + fmt.Sprint(req.Timeout) +
		" -w " + shellQuote("\nHTTPSTATUS:%{http_code}")

	var cfg strings.Builder
	cfg.WriteString("url = \"" + curlConfigEscape(endpoint) + "\"\n")
	cfg.WriteString("request = \"" + curlConfigEscape(req.Method) + "\"\n")
	for _, h := range req.Headers {
		cfg.WriteString("header = \"" + curlConfigEscape(h[0]+": "+h[1]) + "\"\n")
	}
	switch {
	case len(req.Form) > 0:
		form := make([]string, 0, len(req.Form))
		for _, f := range req.Form {
			form = append(form, url.QueryEscape(f[0])+"="+url.QueryEscape(f[1]))
		}
		cfg.WriteString("header = \"Content-Type: application/x-www-form-urlencoded\"\n")
		cfg.WriteString("data = \"" + curlConfigEscape(strings.Join(form, "&")) + "\"\n")
	case req.JSON != "":
		cfg.WriteString("data = \"" + curlConfigEscape(req.JSON) + "\"\n")
	}

	res, err := conn.Exec(ctx, cmd, strings.NewReader(cfg.String()))
	if err != nil {
		return "", 0, err
	}
	if res.RC != 0 {
		return "", 0, fmt.Errorf("curl failed: %s", strings.TrimSpace(res.Stderr))
	}
	return parseCurlStatus(res.Stdout)
}

// curlPostForm is the form-POST case, which is most of them.
func curlPostForm(ctx context.Context, conn remoteexec.Connection, endpoint string, fields [][2]string, timeoutSec int) (body string, status int, err error) {
	return curlDo(ctx, conn, curlReq{Method: "POST", URL: endpoint, Form: fields, Timeout: timeoutSec})
}

// requireCurl fails the module with an explanation rather than a bare
// "command not found", naming why curl is the mechanism at all -- the
// service has no official CLI to shell out to, which is this package's
// usual substitution.
func requireCurl(ctx context.Context, conn remoteexec.Connection, moduleName, service string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v curl"); err != nil {
		return Fail(fmt.Sprintf("%s: the curl binary is required on the target — %s has no official CLI, "+
			"only an HTTP API, so this port posts to the same endpoint real %s.py's own fetch_url calls hit",
			moduleName, service, moduleName)), false
	}
	return Result{}, true
}
