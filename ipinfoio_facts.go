package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpinfoioFacts implements (a subset of) Ansible's
// `ipinfoio_facts` module: gathers the target's own public-IP
// geolocation facts, via ipinfo.io's own official `ipinfo` CLI (repo
// `ipinfo/cli`, self-described "Official Command Line Interface for the
// IPinfo API") instead of real ipinfoio_facts.py's own hand-rolled
// `fetch_url` call against http://ipinfo.io/json.
//
// Real ipinfoio_facts.py takes no meaningful arguments of its own
// (http_agent/timeout only shape its own HTTP client, which this port's
// CLI substitution doesn't use — accepted here for argument-shape
// compatibility but with no effect, since `ipinfo myip` sets its own
// user agent and has no per-invocation HTTP timeout flag). It always
// runs `ipinfo myip --json` (verified against ipinfo/cli's own source,
// ipinfo/cmd_myip.go) and returns the decoded fields directly, matching
// real ipinfoio_facts.py's own ansible_facts shape (ip/hostname/city/
// region/country/loc/org/postal — every field real ipinfoio_facts.py's
// own RETURN documents, `ipinfo myip`'s own JSON output carries too,
// since both ultimately come from the same ipinfo.io API response
// shape).
//
// # Auth
//
// `ipinfo`'s own token has no environment-variable form at all
// (verified against its own source, ipinfo/ipinfo_client.go — only a
// `-t/--token` command-line flag or its own persistent config store
// populated by a prior `ipinfo init`) — this project's own hard "no
// secrets in argv" rule rules out the flag, and real ipinfoio_facts.py
// has no token/api_key argument of its own to even consider wiring
// through. A caller wanting a higher rate limit or bulk-lookup access
// must already have run `ipinfo init` on the target beforehand; without
// one, `ipinfo myip` still works (ipinfo.io's own documented behavior:
// "you can continue without a token, but there will be limited data
// output") — this module does not fail merely because no token is
// configured.
//
// This module never changes anything (Changed is always false), and
// always returns `facts=true`-shaped Extra data under "ansible_facts"
// for the same key real ipinfoio_facts.py's own RETURN documents.
func moduleIpinfoioFacts(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := run(ctx, conn, "command -v ipinfo"); err != nil {
		return Fail("ipinfoio_facts: the ipinfo binary (ipinfo.io's own official CLI) is required on the target " +
			"and was not found in PATH — see ipinfoio_facts.go's own doc comment"), nil
	}

	res, err := conn.Exec(ctx, "ipinfo myip --json", nil)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return Fail(fmt.Sprintf("ipinfoio_facts: ipinfo myip failed: %s", msg)), nil
	}

	var facts map[string]any
	if strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), &facts); jerr != nil {
			return Result{}, fmt.Errorf("ipinfoio_facts: decoding ipinfo myip --json output: %w", jerr)
		}
	}
	return Ok("").WithExtra("ansible_facts", facts), nil
}
