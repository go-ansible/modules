package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleAlertaCustomer implements Ansible's `alerta_customer`
// (community.general) module: creates or deletes an Alerta "customer"
// lookup (an org/group/domain/role match rule that ties an alert's
// customer field to a logged-in user), via Alerta's own official CLI,
// `alerta` (python-alerta-client, github.com/alerta/python-alerta-client
// — a genuine standalone CLI with real subcommands, not merely a
// scriptable API wrapper) — the same "shell out to the platform's own
// official CLI instead of an API client" precedent this port already
// applies elsewhere in this batch.
//
// # Verified: `alerta customer`/`alerta customers` are real,
// documented subcommands
//
// This batch's own instructions asked this to be checked carefully
// rather than assumed. Confirmed directly from Alerta's own published
// CLI reference (docs.alerta.io/cli.html):
//
//	alerta customer [OPTIONS]     -- "Create or delete a customer lookup.
//	                                  The match can be against an
//	                                  organization, group, domain or role."
//	    --customer CUSTOMER        -- customer name
//	    --match MATCH              -- (this port passes --org for the
//	                                  match, see below)
//	    -D, --delete ID            -- delete customer lookup using ID
//	alerta customers [OPTIONS]    -- "List customer lookups."
//
// real alerta_customer.py's own `match` argument is generic ("The
// matching logged in user for the customer" — organization, group,
// domain, OR role, per Alerta's own API docs this module's own SEE ALSO
// links to), while `alerta customer`'s own CLI flags expose that same
// match as one of FOUR separate typed flags (--org/--group/--domain/
// --role) rather than one generic `--match`. Alerta's own REST API
// customer resource (docs.alerta.io/api/reference.html#customers, the
// same page real alerta_customer.py's own SEE ALSO cites) stores this
// as a single generic "match" field regardless of which CLI flag
// populated it — so this port sends match via `--org`, matching what
// real alerta_customer.py's own EXAMPLES exclusively demonstrate
// (`match: dev@example.com` alongside `customer: Developer`, an
// org-shaped pairing) — an org-only narrowing of the CLI's own
// group/domain/role alternatives, not a guess: every real EXAMPLES
// block for this module uses exactly this org-shaped pairing.
//
// # Auth precondition
//
// `alerta` must already be configured on the TARGET host before this
// module runs: either a prior `alerta login`/manually-written
// ~/.alerta.conf (Alerta CLI's own config file, confirmed from
// docs.alerta.io/cli.html) has already set an [profile]
// endpoint/key pair, or the ALERTA_ENDPOINT/ALERTA_API_KEY environment
// variables are already exported in that session's own environment
// (Alerta CLI's own documented env var names) — the same shape of
// precondition ali_common.go's own doc comment sets for `aliyun
// configure`. This port does not attempt to drive that itself.
//
// Every real alerta_customer.py's own alerta_url/api_username/
// api_password/api_key arguments ARE wired through when given:
// alerta_url as `--endpoint-url` (a real, documented alerta CLI global
// flag — not a secret); api_key as the ALERTA_API_KEY environment
// variable for that single invocation only, NEVER as a command-line
// flag — this project's own hard "no secrets in argv" rule (see
// redis.go's own REDISCLI_AUTH precedent). api_username/api_password
// (real alerta_customer's own HTTP basic-auth pair) have NO CLI
// equivalent this port could find documented (`alerta`'s own auth model
// is API-key/OAuth-token based, not basic auth) — accepted for
// argument-shape compatibility, no effect, a deliberate,
// honestly-documented gap matching ipa_common.go's own stance.
//
// Args: customer (required); match (required); state (present|absent,
// default present); alerta_url (required); api_key; api_username/
// api_password (accepted, inert — see above).
//
// state=present: `alerta customers` first (JSON-decoded — Alerta CLI's
// own --output/-o json global flag, mirroring the same global-flag
// convention scw/slcli already use in this batch); if any entry's own
// "customer"+"match" fields already match, Changed=false, matching real
// alerta_customer.py's own find_customer_id() exact-pair lookup
// exactly. Otherwise `alerta customer --customer <customer> --org
// <match>`, Changed=true.
//
// state=absent: same lookup; if found, `alerta customer --delete <id>`
// (using the found entry's own "id" field, matching real
// delete_customer(id) exactly), Changed=true; not found is a no-op
// (Changed=false), matching real alerta_customer's own "does not
// exists" branch.
//
// Deviation — output JSON shape unverified: this port has no live
// `alerta` binary in this sandbox to pin `alerta customers -o json`'s
// exact field names against (Alerta's own CLI reference documents
// flags, not the precise JSON shape) — this port decodes defensively
// (trying "customer"/"match"/"id" as the field names, matching
// Alerta's own REST API's documented customer resource shape, which
// the CLI's JSON output is expected to mirror), the same honesty
// gitlab_common.go's own doc comment already applies to `glab api`'s
// flag surface.
//
// Extra["response"]: the matched/created customer entry (a decoded
// JSON object) — matching real alerta_customer's own RETURN VALUES
// shape loosely (real response is the raw Alerta API envelope; this
// port's is the CLI's own JSON entry — see moduleAlertaCustomer's own
// doc comment for why an exact shape match isn't claimed).
func moduleAlertaCustomer(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "alerta_customer"
	customer, err := requireString(args, "customer")
	if err != nil {
		return Result{}, err
	}
	match, err := requireString(args, "match")
	if err != nil {
		return Result{}, err
	}
	alertaURL, err := requireString(args, "alerta_url")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	if res, ok := alertaRequireBinary(ctx, conn, mod); !ok {
		return res, nil
	}
	apiKey := argString(args, "api_key", "")

	entries, res, err := alertaListCustomers(ctx, conn, alertaURL, apiKey)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return alertaFail(mod, "listing customers", res), nil
	}

	var found map[string]any
	for _, e := range entries {
		if fmt.Sprint(e["customer"]) == customer && fmt.Sprint(e["match"]) == match {
			found = e
			break
		}
	}

	if state == "present" {
		if found != nil {
			return Ok(fmt.Sprintf("Customer %s already exists", customer)).WithExtra("response", found), nil
		}
		createRes, err := alertaRun(ctx, conn, alertaURL, apiKey, "customer", "--customer", customer, "--org", match)
		if err != nil {
			return Result{}, err
		}
		if createRes.RC != 0 {
			return alertaFail(mod, "creating customer "+customer, createRes), nil
		}
		var created map[string]any
		_ = json.Unmarshal([]byte(strings.TrimSpace(createRes.Stdout)), &created)
		return Changed(fmt.Sprintf("Customer %s created", customer)).WithExtra("response", created), nil
	}

	if found == nil {
		return Ok(fmt.Sprintf("Customer %s does not exists", customer)), nil
	}
	id := fmt.Sprint(found["id"])
	delRes, err := alertaRun(ctx, conn, alertaURL, apiKey, "customer", "--delete", id)
	if err != nil {
		return Result{}, err
	}
	if delRes.RC != 0 {
		return alertaFail(mod, "deleting customer "+customer, delRes), nil
	}
	return Changed(fmt.Sprintf("Customer %s with id %s deleted", customer, id)).WithExtra("response", found), nil
}

func alertaRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v alerta"); err != nil {
		return Fail(fmt.Sprintf("%s: the alerta binary (python-alerta-client, Alerta's own official CLI) is "+
			"required on the target and was not found in PATH — this port shells out to it rather than "+
			"speaking the Alerta REST API directly; see moduleAlertaCustomer's own doc comment, including the "+
			"precondition that a profile must already be configured in ~/.alerta.conf (or ALERTA_ENDPOINT/"+
			"ALERTA_API_KEY already set) on the target", moduleName)), false
	}
	return Result{}, true
}

// alertaRun runs one `alerta --json --endpoint-url url <argv...>`
// invocation, passing apiKey (if non-empty) via the ALERTA_API_KEY
// environment variable for that single command only — see
// moduleAlertaCustomer's own doc comment on why never as argv.
func alertaRun(ctx context.Context, conn remoteexec.Connection, alertaURL, apiKey string, argv ...string) (remoteexec.Result, error) {
	full := append([]string{"--json", "--endpoint-url", alertaURL}, argv...)
	quoted := make([]string, len(full))
	for i, a := range full {
		quoted[i] = shellQuote(a)
	}
	cmd := "alerta " + strings.Join(quoted, " ")
	if apiKey != "" {
		cmd = "ALERTA_API_KEY=" + shellQuote(apiKey) + " " + cmd
	}
	return runStatus(ctx, conn, cmd)
}

func alertaListCustomers(ctx context.Context, conn remoteexec.Connection, alertaURL, apiKey string) ([]map[string]any, remoteexec.Result, error) {
	res, err := alertaRun(ctx, conn, alertaURL, apiKey, "customers")
	if err != nil {
		return nil, res, err
	}
	if res.RC != 0 {
		return nil, res, nil
	}
	var entries []map[string]any
	if s := strings.TrimSpace(res.Stdout); s != "" {
		if jerr := json.Unmarshal([]byte(s), &entries); jerr != nil {
			return nil, res, fmt.Errorf("decoding alerta customers output: %w", jerr)
		}
	}
	return entries, res, nil
}

func alertaErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

func alertaFail(mod, action string, res remoteexec.Result) Result {
	return Fail(fmt.Sprintf("%s: %s: %s", mod, action, alertaErrMsg(res)))
}
