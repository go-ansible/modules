package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleApache2ModProxy implements (a subset of) Ansible's
// `apache2_mod_proxy` module: gets or sets an Apache httpd 2.4
// mod_proxy_balancer member's status by fetching and, for a state
// change, POSTing to the balancer-manager status HTML page — read from
// real apache2_mod_proxy.py's own Balancer/BalancerMember classes
// (this batch's hard rule: the exact balancer-manager URL/query-string
// shape and the member-status POST body format are only visible in the
// implementation, not EXAMPLES/OPTIONS).
//
// Architectural deviation, and why: real apache2_mod_proxy issues its
// HTTP requests from the CONTROL node (via fetch_url) and parses the
// response with the beautifulsoup4 Python library. This port has
// neither an HTTP client nor an HTML parser available to Go code
// running on the control node in a way that would reach the actual
// target network the balancer-manager page lives on — matching
// uri.go's own documented reasoning, every request here is issued as a
// `curl` invocation run ON THE TARGET via conn.Exec (see uriCmd/
// parseCurlStatus, reused directly from uri.go), and the returned HTML
// is parsed with regular expressions in this port's own Go code
// instead of a real HTML parser/beautifulsoup4. The balancer-manager
// page's HTML is simple, well-known, generated-by-Apache-itself markup
// (not arbitrary user content), so a regex scrape is a reasonable,
// bounded-risk substitute — but it is a real, documented narrowing:
// nested or reformatted markup that a real HTML parser would still
// handle could confuse this port's regexes. No beautifulsoup4-
// equivalent dependency is added to go.mod for this.
//
// Args: balancer_vhost (string, required); balancer_url_suffix
// (string, default "/balancer-manager/"); member_host (string,
// optional) — omitted means "return every member" (Extra["members"]);
// given means "return (and optionally set) that one member"
// (Extra["member"]); state ([]string, choices: present, absent,
// enabled, disabled, drained, hot_standby, ignore_errors) — "present"
// and "enabled" must not be combined with any other state (matching
// real __init_module__'s own validation); tls (bool, default false) —
// https instead of http; validate_certs (bool, default true) — adds
// curl's `-k` when false.
//
// Status-string parsing/rendering matches real get_member_status/
// set_member_status exactly: the four independent booleans
// disabled/drained/hot_standby/ignore_errors are read from the
// member's raw "Status" attribute text via substring checks ("Dis",
// "Drn", "Stby", "Ign"), and a requested state list is translated to
// those same booleans before being POSTed back as
// "&w_status_D=0|1&w_status_N=0|1&w_status_H=0|1&w_status_I=0|1" —
// state=absent is folded into disabled=true exactly like real code's
// own `elif mode == "disabled" and state == "absent"` special case;
// state=present/enabled leaves every boolean false (enabling the
// member, matching real code's own "no mode matches present/enabled,
// so member_status stays all-false" behavior).
//
// This port has no check_mode support at all (a runtime-engine
// concern outside every module's own Func signature here, not
// specific to this module).
func moduleApache2ModProxy(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	balancerVhost, err := requireString(args, "balancer_vhost")
	if err != nil {
		return Result{}, err
	}
	suffix := argString(args, "balancer_url_suffix", "/balancer-manager/")
	memberHost := argString(args, "member_host", "")
	states := argStringList(args, "state")
	tls := argBool(args, "tls", false)
	validateCerts := argBool(args, "validate_certs", true)

	hasPresentOrEnabled := false
	for _, s := range states {
		if s == "present" || s == "enabled" {
			hasPresentOrEnabled = true
		}
	}
	if len(states) > 1 && hasPresentOrEnabled {
		return Result{}, errArg("apache2_mod_proxy: states present/enabled are mutually exclusive with other states")
	}

	proto := "http"
	if tls {
		proto = "https"
	}
	baseURL := proto + "://" + balancerVhost
	balancerURL := baseURL + suffix

	page, err := apacheModProxyFetch(ctx, conn, balancerURL, validateCerts)
	if err != nil {
		return Result{}, err
	}
	if !apacheVersionOK(page) {
		return Fail("apache2_mod_proxy: could not get the Apache server version from the balancer-manager, or it is not 2.4+"), nil
	}

	hrefs := apacheExtractHrefs(page)
	if len(hrefs) < 2 {
		return Fail("apache2_mod_proxy: no balancer members found on " + balancerURL), nil
	}
	memberHrefs := hrefs[1:] // real code's own elements[1::1]: skips the page's first <a>

	if memberHost == "" {
		var members []any
		for _, href := range memberHrefs {
			m, err := apacheModProxyMember(ctx, conn, baseURL, balancerURL, href, validateCerts)
			if err != nil {
				return Result{}, err
			}
			members = append(members, m)
		}
		return Ok("").WithExtra("members", members), nil
	}

	var found map[string]any
	changed := false
	for _, href := range memberHrefs {
		link, err := apacheParseMemberLink(baseURL, href)
		if err != nil {
			return Result{}, err
		}
		if link.host != memberHost {
			continue // cheap, no-network check: matches real code's own lazy member.status/as_dict()
		}
		m, err := apacheModProxyMemberDict(ctx, conn, balancerURL, link, validateCerts)
		if err != nil {
			return Result{}, err
		}
		found = m
		if len(states) > 0 {
			before := m["status"].(map[string]bool)
			want := apacheModProxyWantStatus(states)
			if !apacheStatusEqual(before, want) {
				managementURL := m["management_url"].(string)
				if err := apacheModProxySetStatus(ctx, conn, managementURL, want, validateCerts); err != nil {
					return Result{}, err
				}
				changed = true
				found["status"] = want
			}
		}
		break
	}
	if found == nil {
		return Fail(fmt.Sprintf("%s is not a member of the balancer %s!", memberHost, balancerVhost)), nil
	}
	r := Ok("")
	if changed {
		r = Changed("")
	}
	return r.WithExtra("member", found), nil
}

// apacheModProxyWantStatus translates a `state` list into the four
// independent status booleans, matching real __run__'s own loop
// exactly (including the state=absent -> disabled=true special case).
func apacheModProxyWantStatus(states []string) map[string]bool {
	want := map[string]bool{"disabled": false, "drained": false, "hot_standby": false, "ignore_errors": false}
	for mode := range want {
		for _, state := range states {
			if mode == state {
				want[mode] = true
			} else if mode == "disabled" && state == "absent" {
				want[mode] = true
			}
		}
	}
	return want
}

func apacheStatusEqual(a, b map[string]bool) bool {
	for k := range a {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

// apacheGetCmd builds a target-side `curl` GET invocation for url, with
// an optional Referer header (real get_member_attributes' own
// `headers={"Referer": self.management_url}`; the top-level balancer
// page fetch sends none) — separated out, matching uriCmd's own
// precedent (uri.go), so tests can build the exact expected command
// without duplicating curl's own flag/quoting details.
func apacheGetCmd(url, referer string, validateCerts bool) string {
	cmd := "curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}")
	if referer != "" {
		cmd += " -H " + shellQuote("Referer: "+referer)
	}
	if !validateCerts {
		cmd += " -k"
	}
	return cmd + " " + shellQuote(url)
}

// apachePostCmd builds a target-side `curl -X POST` invocation for url
// with body as its `-d` payload and a Referer header — matching real
// set_member_status' own `fetch_url(..., data=request_body,
// headers={"Referer": self.management_url})` call.
func apachePostCmd(url, body string, validateCerts bool) string {
	cmd := "curl -s -w " + shellQuote("\nHTTPSTATUS:%{http_code}") +
		" -X POST -H " + shellQuote("Referer: "+url) + " -d " + shellQuote(body)
	if !validateCerts {
		cmd += " -k"
	}
	return cmd + " " + shellQuote(url)
}

// apacheModProxyFetch issues a target-side `curl` GET for url (see this
// file's own doc comment for why the request is issued on the target
// rather than the control node), returning its body. A non-2xx-looking
// status (curl exit or non-200) is a Go error, since a failure to even
// reach the balancer-manager page is an infra problem, not a
// well-formed "the request can't be satisfied" outcome.
func apacheModProxyFetch(ctx context.Context, conn remoteexec.Connection, url string, validateCerts bool) (string, error) {
	return apacheModProxyRunGet(ctx, conn, apacheGetCmd(url, "", validateCerts), url)
}

// apacheModProxyFetchWithReferer is apacheModProxyFetch, but adds a
// Referer header (matching real get_member_attributes' own
// `headers={"Referer": self.management_url}`).
func apacheModProxyFetchWithReferer(ctx context.Context, conn remoteexec.Connection, url string, validateCerts bool) (string, error) {
	return apacheModProxyRunGet(ctx, conn, apacheGetCmd(url, url, validateCerts), url)
}

func apacheModProxyRunGet(ctx context.Context, conn remoteexec.Connection, cmd, url string) (string, error) {
	res, err := runStatus(ctx, conn, cmd)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", fmt.Errorf("apache2_mod_proxy: curl %s: %s", url, strings.TrimSpace(res.Stderr))
	}
	body, status, err := parseCurlStatus(res.Stdout)
	if err != nil {
		return "", fmt.Errorf("apache2_mod_proxy: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("apache2_mod_proxy: could not get %s, HTTP status %d", url, status)
	}
	return body, nil
}

var (
	apacheVersionPattern = regexp.MustCompile(`(?i)SERVER VERSION:\s*APACHE/([0-9.]+)`)
	apacheVersion24      = regexp.MustCompile(`^2\.4\.[0-9]*$`)
	apacheHrefPattern    = regexp.MustCompile(`(?is)<a\b[^>]*\bhref="([^"]*)"`)
	// apacheMemberExprPattern mirrors real EXPRESSION exactly: group 1
	// is the whole "b=...&w=proto://host:port/path&..." match (used to
	// reconstruct a POST request body), 2 the balancer name, 3 the
	// protocol, 4 the host, 5 the port, 6 the path.
	apacheMemberExprPattern = regexp.MustCompile(`(b=([\w.\-]+)&w=(https?|ajp|wss?|ftp|[sf]cgi)://([\w.\-]+):?([0-9]*)([/\w.\-]*)&?[\w\-=]*)`)
	apacheTablePattern      = regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	apacheRowPattern        = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	apacheThPattern         = regexp.MustCompile(`(?is)<th[^>]*>(.*?)</th>`)
	apacheTdPattern         = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	apacheTagPattern        = regexp.MustCompile(`(?s)<[^>]*>`)
)

func apacheVersionOK(page string) bool {
	m := apacheVersionPattern.FindStringSubmatch(page)
	if m == nil {
		return false
	}
	return apacheVersion24.MatchString(m[1])
}

// apacheExtractHrefs returns every <a href="..."> target on page, in
// document order — matching real find_all(soup, "a") (every anchor on
// the page, not just balancer members; the caller skips index 0,
// matching real code's own `elements[1::1]`).
func apacheExtractHrefs(page string) []string {
	matches := apacheHrefPattern.FindAllStringSubmatch(page, -1)
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m[1]
	}
	return out
}

// apacheMemberLink is what can be learned about a balancer member from
// its <a href> alone, with no HTTP request at all — matching real
// BalancerMember.__init__'s own regexp_extraction calls, which read
// host/protocol/port/path straight off management_url without ever
// touching the network (attributes/status are a separate, lazily-
// fetched property in real code).
type apacheMemberLink struct {
	host, protocol, port, path, managementURL string
}

// apacheParseMemberLink extracts host/protocol/port/path from one
// balancer member's href, without fetching anything — used to find
// the member matching member_host cheaply, so this port (unlike a
// careless reimplementation) fetches each non-matching member's own
// attributes page zero times, matching real code's own lazy-property
// behavior (member.host is available from __init__ alone; member.status/
// as_dict() are what actually trigger a fetch, and real __run__ only
// calls those on the one member whose host already matched).
func apacheParseMemberLink(baseURL, href string) (apacheMemberLink, error) {
	m := apacheMemberExprPattern.FindStringSubmatch(href)
	if m == nil {
		return apacheMemberLink{}, fmt.Errorf("apache2_mod_proxy: could not parse balancer member link %q", href)
	}
	return apacheMemberLink{
		host: m[4], protocol: m[3], port: m[5], path: m[6],
		managementURL: baseURL + href,
	}, nil
}

// apacheModProxyMember fetches one balancer member's attributes page
// and returns its as_dict()-shaped map (host/status/protocol/port/
// path/attributes/management_url/balancer_url), matching real
// BalancerMember.as_dict.
func apacheModProxyMember(ctx context.Context, conn remoteexec.Connection, baseURL, balancerURL, href string, validateCerts bool) (map[string]any, error) {
	link, err := apacheParseMemberLink(baseURL, href)
	if err != nil {
		return nil, err
	}
	return apacheModProxyMemberDict(ctx, conn, balancerURL, link, validateCerts)
}

// apacheModProxyMemberDict fetches link's attributes page and builds
// the as_dict()-shaped map — the actual network request, separated
// from apacheParseMemberLink's free parsing (see its own doc comment
// for why that separation matters).
func apacheModProxyMemberDict(ctx context.Context, conn remoteexec.Connection, balancerURL string, link apacheMemberLink, validateCerts bool) (map[string]any, error) {
	page, err := apacheModProxyFetchWithReferer(ctx, conn, link.managementURL, validateCerts)
	if err != nil {
		return nil, err
	}
	attrs, err := apacheParseMemberAttributes(page, link.host)
	if err != nil {
		return nil, err
	}
	status := apacheParseMemberStatus(attrs)

	return map[string]any{
		"host":           link.host,
		"protocol":       link.protocol,
		"port":           link.port,
		"path":           link.path,
		"balancer_url":   balancerURL,
		"management_url": link.managementURL,
		"attributes":     attrs,
		"status":         status,
	}, nil
}

// apacheParseMemberAttributes extracts the balancer-manager page's
// SECOND <table> (matching real `find_all(soup, "table")[1]`), finds
// the row whose raw HTML contains host (matching real code's own
// `re.search(pattern=self.host, string=str(valuesset))`), and zips
// that row's <td> cells against the header row's <th> cells — matching
// real get_member_attributes' own `{keys[x].string: values[x].string
// for x in range(...)}`. A cell with no text after stripping tags
// comes back as nil (matching bs4's `.string` being None for an empty
// or multi-child cell — see this file's own doc comment for the
// broader regex-scrape-vs-beautifulsoup4 deviation this narrows).
func apacheParseMemberAttributes(page, host string) (map[string]any, error) {
	tables := apacheTablePattern.FindAllStringSubmatch(page, -1)
	if len(tables) < 2 {
		return nil, fmt.Errorf("apache2_mod_proxy: expected at least 2 tables on the member attributes page, found %d", len(tables))
	}
	rows := apacheRowPattern.FindAllStringSubmatch(tables[1][1], -1)
	if len(rows) == 0 {
		return nil, fmt.Errorf("apache2_mod_proxy: no rows found in the member attributes table")
	}
	headerCells := apacheThPattern.FindAllStringSubmatch(rows[0][1], -1)
	keys := make([]string, len(headerCells))
	for i, c := range headerCells {
		keys[i] = apacheStripTags(c[1])
	}

	for _, row := range rows[1:] {
		if !strings.Contains(row[1], host) {
			continue
		}
		cells := apacheTdPattern.FindAllStringSubmatch(row[1], -1)
		out := make(map[string]any, len(keys))
		for i, k := range keys {
			if i >= len(cells) {
				out[k] = nil
				continue
			}
			v := apacheStripTags(cells[i][1])
			if v == "" {
				out[k] = nil
			} else {
				out[k] = v
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("apache2_mod_proxy: no row matching host %q found in the member attributes table", host)
}

func apacheStripTags(s string) string {
	s = apacheTagPattern.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	return strings.TrimSpace(s)
}

// apacheParseMemberStatus implements real get_member_status: the
// four independent booleans are substring checks against the member's
// raw "Status" attribute text.
func apacheParseMemberStatus(attrs map[string]any) map[string]bool {
	statusText, _ := attrs["Status"].(string)
	return map[string]bool{
		"disabled":      strings.Contains(statusText, "Dis"),
		"drained":       strings.Contains(statusText, "Drn"),
		"hot_standby":   strings.Contains(statusText, "Stby"),
		"ignore_errors": strings.Contains(statusText, "Ign"),
	}
}

// apacheModProxySetStatus POSTs a member status change, matching real
// set_member_status: the request body is management_url's own
// "b=...&w=...&nonce=..." query substring (re-derived from
// management_url via apacheMemberExprPattern, same as real code's own
// `regexp_extraction(self.management_url, EXPRESSION, 1)`) with
// "&w_status_D=0|1&w_status_N=0|1&w_status_H=0|1&w_status_I=0|1"
// appended in that fixed order.
func apacheModProxySetStatus(ctx context.Context, conn remoteexec.Connection, managementURL string, want map[string]bool, validateCerts bool) error {
	m := apacheMemberExprPattern.FindStringSubmatch(managementURL)
	if m == nil {
		return fmt.Errorf("apache2_mod_proxy: could not derive a request body from %q", managementURL)
	}
	body := apacheStatusPostBody(m[1], want)

	res, err := runStatus(ctx, conn, apachePostCmd(managementURL, body, validateCerts))
	if err != nil {
		return err
	}
	if res.RC != 0 {
		return fmt.Errorf("apache2_mod_proxy: curl %s: %s", managementURL, strings.TrimSpace(res.Stderr))
	}
	_, status, err := parseCurlStatus(res.Stdout)
	if err != nil {
		return fmt.Errorf("apache2_mod_proxy: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("apache2_mod_proxy: could not set the member status, HTTP status %d", status)
	}
	return nil
}

func apacheStatusPostBody(prefix string, want map[string]bool) string {
	order := []struct{ mode, param string }{
		{"disabled", "w_status_D"},
		{"drained", "w_status_N"},
		{"hot_standby", "w_status_H"},
		{"ignore_errors", "w_status_I"},
	}
	var b strings.Builder
	b.WriteString(prefix)
	for _, o := range order {
		v := 0
		if want[o.mode] {
			v = 1
		}
		fmt.Fprintf(&b, "&%s=%d", o.param, v)
	}
	return b.String()
}
