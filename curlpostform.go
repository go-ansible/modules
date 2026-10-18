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
func curlPostForm(ctx context.Context, conn remoteexec.Connection, endpoint string, fields [][2]string, timeoutSec int) (body string, status int, err error) {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	form := make([]string, 0, len(fields))
	for _, f := range fields {
		form = append(form, url.QueryEscape(f[0])+"="+url.QueryEscape(f[1]))
	}

	// -s so curl's progress meter does not land in stdout beside the
	// body; -w appends the status in the shape parseCurlStatus reads,
	// the same marker uri.go and infinity.go already use.
	cmd := "curl -s -K - --max-time " + fmt.Sprint(timeoutSec) +
		" -w " + shellQuote("\nHTTPSTATUS:%{http_code}")

	var cfg strings.Builder
	cfg.WriteString("url = \"" + curlConfigEscape(endpoint) + "\"\n")
	cfg.WriteString("request = \"POST\"\n")
	cfg.WriteString("header = \"Content-Type: application/x-www-form-urlencoded\"\n")
	cfg.WriteString("data = \"" + curlConfigEscape(strings.Join(form, "&")) + "\"\n")

	res, err := conn.Exec(ctx, cmd, strings.NewReader(cfg.String()))
	if err != nil {
		return "", 0, err
	}
	if res.RC != 0 {
		return "", 0, fmt.Errorf("curl failed: %s", strings.TrimSpace(res.Stderr))
	}
	return parseCurlStatus(res.Stdout)
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
