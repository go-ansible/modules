package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the two dnsimple*.go modules in this batch
// share: shelling out to DNSimple's own official `dnsimple` CLI (repo
// `dnsimple/cli`) instead of the `dnsimple-python` API client real
// dnsimple.py/dnsimple_info.py use — the same "shell out to the
// platform's own official CLI instead of an API client" precedent this
// port already uses elsewhere (see github_common.go's own doc comment
// for the fuller rationale).
//
// # A brand-new tool, verified from its own source, not assumed
//
// This CLI was RELAUNCHED in May 2026 (DNSimple's own blog post,
// "Introducing the DNSimple CLI: Manage Your DNS from the Command
// Line") after an earlier community `dnsimple-python`-based CLI had
// gone dead — it has only a few months of track record as of this
// batch's own research, unlike the long-established `gh`/`glab`/
// `kcadm.sh` this port already wraps elsewhere. Every command shape
// below was verified directly against dnsimple/cli's own Go source
// (internal/cli/zones_records.go, internal/cli/domains.go,
// internal/cli/root.go, internal/cmdutil/factory.go) on GitHub, not
// guessed from the module names or from the older dead tool's own
// conventions — but a tool this new may still change shape faster than
// a long-established one; a caller pinning an exact `dnsimple` CLI
// version in their own fleet is not paranoia here.
//
// # Auth: account_api_token wires through, account_email does not
//
// `dnsimple`'s own global flags are `--token` (falls back to the
// DNSIMPLE_TOKEN environment variable) and `--account`/`-a` (falls back
// to the DNSIMPLE_ACCOUNT environment variable, a persisted default
// account, or a prior `dnsimple auth login`) — verified directly against
// factory.go's own GlobalFlags/AccountID resolution. This port wires
// account_api_token through as the DNSIMPLE_TOKEN environment variable
// for each single `dnsimple` invocation (never as a `--token` argv flag
// — this project's own hard "no secrets in argv" rule) and domain
// (which doubles as the account selector for the domain-less "list all
// domains" call — see dnsimple.go's own doc comment) is passed as a
// plain positional argument, never a secret.
//
// account_email is accepted by both modules in this batch (for
// argument-shape compatibility with real playbooks written against
// real dnsimple.py/dnsimple_info.py) but has NO EFFECT here: the
// dnsimple-python client real dnsimple.py used accepted an email
// alongside a token as a historical artifact of its own v1-API
// email+token auth scheme (still present in its v2 client's
// constructor signature but unused by the token-only v2 API this port's
// own `dnsimple` CLI talks to exclusively) — a documented gap, not a
// silent misinterpretation. account_id (dnsimple_info's own required
// argument) is likewise NOT wired through as `--account`: this port
// always lets `dnsimple` resolve its own account from DNSIMPLE_TOKEN/
// DNSIMPLE_ACCOUNT/a prior `dnsimple auth login`, exactly as if no
// `--account` flag had been given by a human operator either — a
// caller whose token has access to more than one account must already
// have DNSIMPLE_ACCOUNT set (or a default configured via `dnsimple auth
// login`) on the target for this port's dnsimple_info.go to resolve the
// same account dnsimple_info.py's own account_id argument would have
// selected explicitly.
//
// # sandbox wires through directly
//
// Both modules' own `sandbox` bool maps directly to `dnsimple`'s own
// `--sandbox` global flag (verified in root.go) — a genuine, exact
// match, unlike the auth arguments above.
//
// # Output shape: always "--json", always a {"data": ...} envelope
//
// Every dnsimple invocation this port makes passes `--json` (verified:
// root.go's own `--json` global flag). A single-resource command
// (`domains get`, `records get`) wraps its object under a `data` key
// (domains.go's own domainItem.JSONData/recordItem.JSONData); a listing
// command (`domains list`, `records list`) wraps an array the same way,
// alongside an optional `pagination` key this port never follows (every
// domain/record volume this batch's modules realistically manage fits
// on dnsimple-cli's own default single page; a caller managing enough
// records to paginate needs functionality outside this port's own
// scope). dnsimpleRunJSON/dnsimpleRunJSONList below decode exactly that
// shape.
func dnsimpleRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v dnsimple"); err != nil {
		return Fail(fmt.Sprintf("%s: the dnsimple binary (DNSimple's own official CLI, relaunched May 2026 — see "+
			"dnsimple_common.go's own doc comment) is required on the target and was not found in PATH — this port "+
			"shells out to it rather than speaking the DNSimple API via dnsimple-python directly; a token with "+
			"sufficient access must already be available via DNSIMPLE_TOKEN or a prior `dnsimple auth login` on "+
			"the target", moduleName)), false
	}
	return Result{}, true
}

// dnsimpleCmd renders one `dnsimple <argv...> --json [--sandbox]`
// invocation, shell-quoting each argv entry, prefixed with
// DNSIMPLE_TOKEN=<token> when token is non-empty.
func dnsimpleCmd(token string, sandbox bool, argv ...string) string {
	full := append(append([]string{}, argv...), "--json")
	if sandbox {
		full = append(full, "--sandbox")
	}
	quoted := make([]string, len(full))
	for i, a := range full {
		quoted[i] = shellQuote(a)
	}
	cmd := "dnsimple " + strings.Join(quoted, " ")
	if token != "" {
		cmd = "DNSIMPLE_TOKEN=" + shellQuote(token) + " " + cmd
	}
	return cmd
}

func dnsimpleRun(ctx context.Context, conn remoteexec.Connection, token string, sandbox bool, argv ...string) (remoteexec.Result, error) {
	return conn.Exec(ctx, dnsimpleCmd(token, sandbox, argv...), nil)
}

// dnsimpleItemEnvelope is the {"data": {...}} shape every single-
// resource dnsimple JSON output uses.
type dnsimpleItemEnvelope struct {
	Data map[string]any `json:"data"`
}

// dnsimpleListEnvelope is the {"data": [...], "pagination": {...}}
// shape every listing dnsimple JSON output uses.
type dnsimpleListEnvelope struct {
	Data []map[string]any `json:"data"`
}

// dnsimpleRunItem runs argv and decodes a single-resource {"data":
// {...}} response. A non-zero exit is reported back as-is (found=false,
// nil error) — matching this port's own kcadmShow/ipaShow convention
// that a missing DNSimple resource is an expected, common outcome, not
// an infrastructure failure.
func dnsimpleRunItem(ctx context.Context, conn remoteexec.Connection, token string, sandbox bool, argv ...string) (item map[string]any, found bool, res remoteexec.Result, err error) {
	res, err = dnsimpleRun(ctx, conn, token, sandbox, argv...)
	if err != nil {
		return nil, false, res, err
	}
	if res.RC != 0 {
		return nil, false, res, nil
	}
	var env dnsimpleItemEnvelope
	if strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), &env); jerr != nil {
			return nil, false, res, fmt.Errorf("decoding dnsimple %v output: %w", argv, jerr)
		}
	}
	return env.Data, true, res, nil
}

// dnsimpleRunList runs argv and decodes a listing {"data": [...]}
// response.
func dnsimpleRunList(ctx context.Context, conn remoteexec.Connection, token string, sandbox bool, argv ...string) ([]map[string]any, remoteexec.Result, error) {
	res, err := dnsimpleRun(ctx, conn, token, sandbox, argv...)
	if err != nil {
		return nil, res, err
	}
	if res.RC != 0 {
		return nil, res, nil
	}
	var env dnsimpleListEnvelope
	if strings.TrimSpace(res.Stdout) != "" {
		if jerr := json.Unmarshal([]byte(res.Stdout), &env); jerr != nil {
			return nil, res, fmt.Errorf("decoding dnsimple %v output: %w", argv, jerr)
		}
	}
	return env.Data, res, nil
}

func dnsimpleErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
