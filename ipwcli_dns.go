package modules

import (
	"context"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleIpwcliDns implements Ansible's `ipwcli_dns`
// (community.general) module: creates or removes an A/AAAA/SRV/NAPTR
// DNS record on an Ericsson IPWorks DNS server, by driving `ipwcli` —
// IPWorks' own INTERACTIVE administration CLI — over stdin. Unlike
// every other module in this batch, real ipwcli_dns.py already shells
// out to a CLI itself (not a REST/SDK client this port is substituting
// a CLI for): this port is a direct, faithful port of real
// ipwcli_dns.py's own ResourceRecord class onto this port's own
// conn.Exec-based architecture, replicating its actual
// stdin-scripting approach exactly (read directly from real
// ipwcli_dns.py's own source — the record syntax, the
// found/create/delete conditions, and the login-failure/creation-
// failure detection strings below are ALL taken verbatim from it, not
// guessed).
//
// # `ipwcli` invocation shape (verbatim from real ipwcli_dns.py)
//
// `ipwcli -user=<username> -password=<password>`, with the record
// operation itself piped over STDIN as one line — `ipwcli` is
// interactive-only and has no non-interactive flag form for these
// operations, so scripting it via stdin (matching real
// module.run_command(cmd, data=stdin)) is the only way to drive it at
// all. The record's own textual representation, sent verbatim as
// real ResourceRecord.create_arecord/create_srvrecord/create_naptrrecord
// builds it:
//
//	arecord <dnsname> <address> -set ttl=<ttl>;container=<container>
//	aaaarecord <dnsname> <address> -set ttl=<ttl>;container=<container>
//	srvrecord <dnsname> -set ttl=<ttl>;container=<container>;priority=<p>;weight=<w>;port=<port>;target=<target>
//	naptrrecord <dnsname> -set ttl=<ttl>;container=<container>;order=<o>;preference=<p>;flags="<f>";service="<s>";replacement="<r>"
//
// stdin for the three operations this port issues is exactly that
// record text prefixed by the verb: `create <record>` (deploy),
// `list <record with ;set-> replaced by &&/where>` (existence probe),
// `delete <same &&/where-rewritten form>` (remove) — matching real
// deploy_record/list_record/delete_record exactly, including their
// own literal `.replace(";", "&&").replace("set", "where")`
// transformation for list/delete.
//
// # Deviation from this project's own "no secrets in argv" rule —
// deliberate, matching real ipwcli_dns.py's own behavior exactly
//
// Real ipwcli_dns.py places `-password=<password>` directly on
// `ipwcli`'s own argv (module.run_command's own argv list, not piped
// over stdin) — this port matches that exactly rather than inventing
// an unverified alternative. `ipwcli`'s own documented invocation
// syntax was not found to support a password via stdin/prompt/
// environment variable anywhere in real ipwcli_dns.py's own source or
// its own NOTES (only `update dnsserver`, its own post-change
// activation command, runs over stdin at all) — this port replicates
// real ipwcli_dns.py's own choice faithfully as an inherited
// limitation of the target CLI, not a gap this port introduces; see
// moduleIpwcliDns's own doc comment for why this is the one place in
// this whole batch a credential appears on a composed command line.
//
// Args: dnsname (required); type (required, choices A|AAAA|SRV|NAPTR);
// container (required); address (required for type=A/AAAA); ttl
// (default 3600); state (present|absent, default present); priority
// (default 10, SRV); weight (default 10, SRV); port (required for
// type=SRV); target (required for type=SRV); order/preference/service/
// replacement (required for type=NAPTR); flags (choices S|A|U|P,
// required for type=NAPTR); username (required); password (required).
//
// state=present: `list <record>` first; if NOT already found, `create
// <record>` (Changed=true) — a record already present is a no-op.
// state=absent: `list <record>` first; if found, `delete <record>`
// (Changed=true) — a record already absent is a no-op. Matching real
// ipwcli_dns.py's own run_module() exactly.
//
// "Invalid username or password" anywhere in ipwcli's own stdout (real
// ipwcli_dns.py checks stdout, not stderr — `ipwcli` reports its own
// login failure there) is a Fail (Result{Failed:true}), on every call
// this port makes (list/create/delete alike), matching real
// ipwcli_dns.py's own access-denied check at every one of its own
// three call sites. A create is only treated as successful if ipwcli's
// own stdout contains the literal "1 object(s) created." (matching
// real deploy_record exactly); a delete only if it contains "1
// object(s) were updated." (matching real delete_record exactly, an
// odd-but-real wording — ipwcli reports a record removal as an
// "update", not real ipwcli_dns.py's own bug, its own faithfully
// preserved real server response text) — anything else on either is a
// Fail.
//
// Extra["record"]: the record's own textual representation (the
// create/delete verb's own line minus the verb) — matching real
// ipwcli_dns's own RETURN VALUES exactly.
func moduleIpwcliDns(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "ipwcli_dns"
	dnsname, err := requireString(args, "dnsname")
	if err != nil {
		return Result{}, err
	}
	dnstype, err := requireString(args, "type")
	if err != nil {
		return Result{}, err
	}
	container, err := requireString(args, "container")
	if err != nil {
		return Result{}, err
	}
	username, err := requireString(args, "username")
	if err != nil {
		return Result{}, err
	}
	password, err := requireString(args, "password")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	if state != "present" && state != "absent" {
		return Result{}, errArg("%s: state must be one of present, absent, got %q", mod, state)
	}
	ttl := argInt(args, "ttl", 3600)

	var record string
	switch dnstype {
	case "A", "AAAA":
		address, err := requireString(args, "address")
		if err != nil {
			return Result{}, errArg("%s: address is required for type=%s", mod, dnstype)
		}
		verb := "arecord"
		if dnstype == "AAAA" {
			verb = "aaaarecord"
		}
		record = fmt.Sprintf("%s %s %s -set ttl=%d;container=%s", verb, dnsname, address, ttl, container)
	case "SRV":
		port, ok := args["port"]
		if !ok {
			return Result{}, errArg("%s: port is required for type=SRV", mod)
		}
		target, err := requireString(args, "target")
		if err != nil {
			return Result{}, errArg("%s: target is required for type=SRV", mod)
		}
		priority := argInt(args, "priority", 10)
		weight := argInt(args, "weight", 10)
		record = fmt.Sprintf("srvrecord %s -set ttl=%d;container=%s;priority=%d;weight=%d;port=%s;target=%s",
			dnsname, ttl, container, priority, weight, fmt.Sprint(port), target)
	case "NAPTR":
		order, ok := args["order"]
		if !ok {
			return Result{}, errArg("%s: order is required for type=NAPTR", mod)
		}
		preference, ok := args["preference"]
		if !ok {
			return Result{}, errArg("%s: preference is required for type=NAPTR", mod)
		}
		service, err := requireString(args, "service")
		if err != nil {
			return Result{}, errArg("%s: service is required for type=NAPTR", mod)
		}
		replacement, err := requireString(args, "replacement")
		if err != nil {
			return Result{}, errArg("%s: replacement is required for type=NAPTR", mod)
		}
		flags, err := requireString(args, "flags")
		if err != nil {
			return Result{}, errArg("%s: flags is required for type=NAPTR", mod)
		}
		record = fmt.Sprintf(`naptrrecord %s -set ttl=%d;container=%s;order=%s;preference=%s;flags="%s";service="%s";replacement="%s"`,
			dnsname, ttl, container, fmt.Sprint(order), fmt.Sprint(preference), flags, service, replacement)
	default:
		return Result{}, errArg("%s: type must be one of A, AAAA, SRV, NAPTR, got %q", mod, dnstype)
	}

	found, listOut, err := ipwcliList(ctx, conn, username, password, dnsname, dnstype, record)
	if err != nil {
		return Result{}, err
	}
	if strings.Contains(listOut, "Invalid username or password") {
		return Fail(mod + ": access denied at ipwcli login: Invalid username or password"), nil
	}

	if found && state == "absent" {
		out, err := ipwcliRun(ctx, conn, username, password, "delete "+ipwcliWhereForm(record))
		if err != nil {
			return Result{}, err
		}
		if strings.Contains(out, "Invalid username or password") {
			return Fail(mod + ": access denied at ipwcli login: Invalid username or password"), nil
		}
		if !strings.Contains(out, "1 object(s) were updated.") {
			return Fail(mod + ": record deletion failed: " + strings.TrimSpace(out)), nil
		}
		return Changed("").WithExtra("record", record), nil
	}

	if !found && state == "present" {
		out, err := ipwcliRun(ctx, conn, username, password, "create "+record)
		if err != nil {
			return Result{}, err
		}
		if strings.Contains(out, "Invalid username or password") {
			return Fail(mod + ": access denied at ipwcli login: Invalid username or password"), nil
		}
		if !strings.Contains(out, "1 object(s) created.") {
			return Fail(mod + ": record creation failed: " + strings.TrimSpace(out)), nil
		}
		return Changed("").WithExtra("record", record), nil
	}

	return Ok("").WithExtra("record", record), nil
}

// ipwcliWhereForm matches real list_record/delete_record's own
// `record.replace(";", "&&").replace("set", "where")` transformation
// exactly.
func ipwcliWhereForm(record string) string {
	return strings.ReplaceAll(strings.ReplaceAll(record, ";", "&&"), "set", "where")
}

// ipwcliRun runs `ipwcli -user=<username> -password=<password>`,
// piping stdin to it, and returns its raw stdout — matching real
// ResourceRecord's own module.run_command(cmd, data=stdin) exactly,
// including placing password directly on argv (see moduleIpwcliDns's
// own doc comment on why that inherited limitation is preserved
// faithfully rather than silently "fixed").
func ipwcliRun(ctx context.Context, conn remoteexec.Connection, username, password, stdin string) (string, error) {
	argv := []string{"ipwcli", "-user=" + username, "-password=" + password}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	cmd := strings.Join(quoted, " ")
	res, err := conn.Exec(ctx, cmd, strings.NewReader(stdin))
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// ipwcliList runs `list <where-form record>` and reports whether the
// record already exists — matching real list_record's own per-type
// "ARecord <dnsname>"/"SRVRecord <dnsname>"/"NAPTRRecord <dnsname>"
// substring check in ipwcli's own output exactly (case-sensitive,
// exactly as real ipwcli_dns.py spells it).
func ipwcliList(ctx context.Context, conn remoteexec.Connection, username, password, dnsname, dnstype, record string) (bool, string, error) {
	out, err := ipwcliRun(ctx, conn, username, password, "list "+ipwcliWhereForm(record))
	if err != nil {
		return false, "", err
	}
	var marker string
	switch dnstype {
	case "A", "AAAA":
		marker = "ARecord " + dnsname
	case "SRV":
		marker = "SRVRecord " + dnsname
	case "NAPTR":
		marker = "NAPTRRecord " + dnsname
	}
	return strings.Contains(out, marker), out, nil
}
