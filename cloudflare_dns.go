package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleCloudflareDns implements (a subset of) Ansible's
// `cloudflare_dns` module: manages DNS records via `flarectl`, the
// Cloudflare-GO project's own CLI (github.com/cloudflare/cloudflare-go/
// cmd/flarectl) instead of real cloudflare_dns.py's own hand-rolled
// `fetch_url` calls against Cloudflare's REST API directly.
//
// # A real, vendor-published tool — but secondary, not Cloudflare's
// # primary CLI
//
// `flarectl` genuinely ships from Cloudflare's own `cloudflare-go`
// GitHub org repository (github.com/cloudflare/cloudflare-go/
// cmd/flarectl) and its own DNS subcommands (`dns list/create/update/
// delete`) are real, current, and documented in that repo. It is NOT
// the same thing as `wrangler` (Cloudflare's actively-marketed primary
// CLI, which is Workers/Pages-focused and has no generic zone DNS
// record management at all) — `flarectl` is a thinner, less
// actively-promoted reference tool bundled with the Go API client
// library, positioned by Cloudflare itself as secondary tooling. This
// port uses it anyway because it is the one OFFICIAL Cloudflare-
// published binary that actually does what this module needs
// (`wrangler` cannot), matching this batch's own "shell out to the
// platform's own official CLI instead of an API client" precedent —
// with this nuance honestly noted rather than implied to be
// Cloudflare's flagship tool.
//
// # Record types supported
//
// `flarectl dns create/update` (verified against cloudflare-go's own
// source, cmd/flarectl/dns.go) takes only a FLAT `name`/`type`/
// `content`/`ttl`/`proxy`/`priority` shape — there is no equivalent of
// real cloudflare_dns.py's own structured per-type `data` object
// (srv_data/ds_data/sshfp_data/tlsa_data/caa_data). This port therefore
// only supports the record types that fit that flat shape: A, AAAA,
// CNAME, MX, NS, TXT, PTR. A request for SRV, DS, SSHFP, TLSA, or CAA
// is a Fail — an honestly-documented gap, not a silent
// misinterpretation; `comment`/`tags` (also real-module-only fields
// with no flarectl equivalent) are accepted for argument-shape
// compatibility but have no effect.
//
// # Auth
//
// api_token (preferred) is exported as CF_API_TOKEN for the single
// `flarectl` invocation; otherwise account_api_key/account_api_token
// (alias) + account_email are exported as CF_API_KEY/CF_API_EMAIL —
// never as command-line flags, matching flarectl's own documented
// environment-variable auth exactly (this project's own hard "no
// secrets in argv" rule) and real cloudflare_dns.py's own three-way
// api_token-vs-account_api_key+account_email choice.
//
// # Idempotency
//
// `flarectl --json dns list --zone <zone> --type <type> --name
// <record>` lists candidates; for CNAME (which Cloudflare allows only
// one of per name) the content is ignored while searching, matching
// real cloudflare_dns.py's own "there can only be one CNAME per record"
// comment — this lets an existing CNAME's target be updated rather than
// creating a duplicate. Every other supported type matches on
// name+type+content exactly. A match whose ttl/proxied(A/AAAA/CNAME
// only)/priority(MX only) differs from what's requested is updated via
// `flarectl dns update --id <id> ...`; no match creates one via
// `flarectl dns create ...`. state=absent removes every record matching
// name+type (+content, unless solo=true, in which case content is
// ignored so every record sharing that name+type is removed) —
// documented as a simplification of real cloudflare_dns.py's own more
// intricate solo-vs-non-solo deletion interaction, which this port does
// not attempt to replicate exactly.
func moduleCloudflareDns(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := run(ctx, conn, "command -v flarectl"); err != nil {
		return Fail("cloudflare_dns: the flarectl binary (Cloudflare's own reference CLI from " +
			"cloudflare/cloudflare-go) is required on the target and was not found in PATH — see " +
			"cloudflare_dns.go's own doc comment"), nil
	}

	zone := argString(args, "zone", argString(args, "domain", ""))
	if zone == "" {
		return Result{}, errArg("cloudflare_dns: zone is required")
	}
	recordType := argString(args, "type", "")
	state := argString(args, "state", "present")
	if state == "present" && recordType == "" {
		return Result{}, errArg("cloudflare_dns: type is required when state=present")
	}
	switch recordType {
	case "SRV", "DS", "SSHFP", "TLSA", "CAA":
		return Fail(fmt.Sprintf("cloudflare_dns: record type %s is not supported by this port's flarectl-based "+
			"substitution (flarectl has no structured per-type data field) — see cloudflare_dns.go's own doc comment", recordType)), nil
	}

	record := argString(args, "record", argString(args, "name", "@"))
	if record == "@" {
		record = zone
	}
	if !strings.HasSuffix(record, zone) {
		record = record + "." + zone
	}
	value := argString(args, "value", argString(args, "content", ""))
	if state == "present" && value == "" {
		return Result{}, errArg("cloudflare_dns: value is required when state=present")
	}
	ttl := argInt(args, "ttl", 1)
	proxied := argBool(args, "proxied", false)
	priority := argInt(args, "priority", 1)
	solo := argBool(args, "solo", false)

	env := cloudflareAuthEnv(args)

	searchValue := value
	if recordType == "CNAME" {
		searchValue = ""
	}
	records, res, err := cloudflareDNSList(ctx, conn, env, zone, recordType, record, searchValue)
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("cloudflare_dns: " + cloudflareErrMsg(res)), nil
	}

	if state == "absent" {
		toDelete := records
		if !solo {
			var filtered []map[string]any
			for _, r := range records {
				if fmt.Sprint(r["content"]) == value {
					filtered = append(filtered, r)
				}
			}
			toDelete = filtered
		}
		if len(toDelete) == 0 {
			return Ok("no matching record"), nil
		}
		for _, r := range toDelete {
			if _, err := cloudflareRun(ctx, conn, env, "dns", "delete", "--zone", zone, "--id", fmt.Sprint(r["id"])); err != nil {
				return Result{}, err
			}
		}
		return Changed(fmt.Sprintf("removed %d record(s)", len(toDelete))), nil
	}

	// state == "present"
	if len(records) > 1 {
		return Fail("cloudflare_dns: more than one record already exists for the given attributes"), nil
	}
	if len(records) == 1 {
		cur := records[0]
		needsUpdate := argIntFromAny(cur["ttl"]) != ttl
		if recordType == "A" || recordType == "AAAA" || recordType == "CNAME" {
			if curProxied, ok := cur["proxied"].(bool); ok && curProxied != proxied {
				needsUpdate = true
			}
		}
		if recordType == "MX" {
			if argIntFromAny(cur["priority"]) != priority {
				needsUpdate = true
			}
		}
		if recordType == "CNAME" && fmt.Sprint(cur["content"]) != value {
			needsUpdate = true
		}
		if !needsUpdate {
			return Ok(record+" unchanged").WithExtra("record", cur), nil
		}
		updateArgv := []string{"dns", "update", "--zone", zone, "--id", fmt.Sprint(cur["id"]), "--name", record,
			"--type", recordType, "--content", value, "--ttl", strconv.Itoa(ttl)}
		if recordType == "A" || recordType == "AAAA" || recordType == "CNAME" {
			if proxied {
				updateArgv = append(updateArgv, "--proxy")
			}
		}
		if recordType == "MX" {
			updateArgv = append(updateArgv, "--priority", strconv.Itoa(priority))
		}
		if _, err := cloudflareRun(ctx, conn, env, updateArgv...); err != nil {
			return Result{}, err
		}
		return Changed(record + " updated"), nil
	}

	createArgv := []string{"dns", "create", "--zone", zone, "--name", record, "--type", recordType,
		"--content", value, "--ttl", strconv.Itoa(ttl)}
	if recordType == "A" || recordType == "AAAA" || recordType == "CNAME" {
		if proxied {
			createArgv = append(createArgv, "--proxy")
		}
	}
	if recordType == "MX" {
		createArgv = append(createArgv, "--priority", strconv.Itoa(priority))
	}
	if _, err := cloudflareRun(ctx, conn, env, createArgv...); err != nil {
		return Result{}, err
	}
	return Changed(record + " created"), nil
}

// cloudflareAuthEnv builds the environment-variable prefix (never argv
// flags) for one flarectl invocation, preferring api_token over
// account_api_key/account_api_token+account_email.
func cloudflareAuthEnv(args map[string]any) map[string]string {
	if token := argString(args, "api_token", ""); token != "" {
		return map[string]string{"CF_API_TOKEN": token}
	}
	key := argString(args, "account_api_key", argString(args, "account_api_token", ""))
	email := argString(args, "account_email", "")
	env := map[string]string{}
	if key != "" {
		env["CF_API_KEY"] = key
	}
	if email != "" {
		env["CF_API_EMAIL"] = email
	}
	return env
}

func cloudflareCmd(env map[string]string, argv ...string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	prefix := ""
	for _, k := range []string{"CF_API_TOKEN", "CF_API_KEY", "CF_API_EMAIL"} {
		if v, ok := env[k]; ok {
			prefix += k + "=" + shellQuote(v) + " "
		}
	}
	return prefix + "flarectl --json " + strings.Join(quoted, " ")
}

func cloudflareRun(ctx context.Context, conn remoteexec.Connection, env map[string]string, argv ...string) (remoteexec.Result, error) {
	return conn.Exec(ctx, cloudflareCmd(env, argv...), nil)
}

// cloudflareDNSList runs `flarectl --json dns list --zone Z --type T
// --name N [--content C]` and decodes the resulting JSON array.
func cloudflareDNSList(ctx context.Context, conn remoteexec.Connection, env map[string]string, zone, recordType, name, content string) ([]map[string]any, remoteexec.Result, error) {
	argv := []string{"dns", "list", "--zone", zone}
	if recordType != "" {
		argv = append(argv, "--type", recordType)
	}
	if name != "" {
		argv = append(argv, "--name", name)
	}
	if content != "" {
		argv = append(argv, "--content", content)
	}
	res, err := cloudflareRun(ctx, conn, env, argv...)
	if err != nil {
		return nil, res, err
	}
	if res.RC != 0 || strings.TrimSpace(res.Stdout) == "" {
		return nil, res, nil
	}
	var records []map[string]any
	if jerr := json.Unmarshal([]byte(res.Stdout), &records); jerr != nil {
		return nil, res, fmt.Errorf("decoding flarectl dns list output: %w", jerr)
	}
	return records, res, nil
}

func cloudflareErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
