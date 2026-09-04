package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleOnepasswordInfo implements Ansible's `onepassword_info`
// (community.general) module: fetches one or more item fields from
// 1Password. Unlike every other module in this batch, real
// onepassword_info.py ALREADY wraps the official `op` 1Password CLI
// itself (not a REST API client this port is substituting a CLI for)
// — this port is a direct, faithful port of real onepassword_info.py's
// own OnePasswordInfo class onto this port's own conn.Exec-based
// architecture, not an architectural substitution. Read directly from
// real onepassword_info.py's own source (module_utils/_onepassword.py's
// OnePasswordConfig is not needed here — this port has no local
// filesystem on the control node to probe for op's own config file the
// way real get_token()'s `os.path.isfile(self._config.config_file_path)`
// does; see "Sign-in" below for how this port narrows that instead).
//
// # `op` CLI syntax used (verified from real onepassword_info.py's own
// source, targeting the LEGACY op CLI — v0.x/v1.x — real
// onepassword_info.py's own NOTES says "Tested with op version 0.5.5";
// the modern op CLI v2 uses different verbs, `op item get`/`op read`,
// which real onepassword_info.py does NOT use)
//
//	op get account                          -- probes whether already signed in
//	op get item <name> [--vault=<vault>]     -- fetches one item's JSON
//	op get document <title>                  -- fetches a document attachment's raw content
//	op signin <subdomain>.1password.com <username> <secret_key> --output=raw   -- initial sign-in, master_password on stdin
//	op signin [<subdomain>] --output=raw     -- subsequent sign-in, master_password on stdin
//	--session=<token>                        -- appended to every op call once signed in this run
//
// Args: search_terms (required, list of string-or-dict) — each entry
// is {name (required), field (default "password"), section, vault};
// a plain string entry is shorthand for {name: <string>}; cli_path
// (default "op") — this port's own aliOfCLI is simply the literal
// command name/path run via the target Connection, matching real
// onepassword_info's own module.run_command([cli_path, ...]) exactly;
// auto_login (dict: subdomain, username, secret_key, master_password)
// — when given, this port attempts a sign-in exactly like real
// onepassword_info's own full_login()/get_token() before running any
// search term.
//
// # Sign-in — narrowed from real onepassword_info.py
//
// Real onepassword_info.py first checks whether op's own LOCAL config
// file already exists (os.path.isfile) to decide between a "quick"
// signin (master_password only, reusing a cached device/account) and a
// full signin (subdomain+username+secret_key+master_password, a fresh
// device registration). This port has no equivalent local-file probe
// available through a Connection's own primitives in a way that
// distinguishes "never signed in on this target before" from "signed
// in before, but the session merely expired" — so it always tries `op
// get account` FIRST (matching real assert_logged_in's own first
// check) and, only if that fails, attempts ONE signin: full (all four
// auto_login fields) when username/secret_key/subdomain are ALL given,
// otherwise the shorter master_password-only form (subdomain optional)
// — matching real get_token()'s own two branches, just without the
// config-file-existence pre-check steering which one real
// onepassword_info.py tries first. This is a documented, narrow
// deviation: a target that has NEVER been signed in before but is
// given only master_password (no username/secret_key) fails here with
// op's own real error text (a legitimate "insufficient info to sign
// in" failure) exactly where real onepassword_info.py would instead
// have inferred "this must be a full signin" from the missing config
// file and failed its own OWN pre-flight check first — the end result
// (a clear failure demanding full auto_login fields) is the same,
// only the error text's origin differs.
//
// master_password (and the initial signin's secret_key/username) are
// piped over stdin, never placed on the command line or in an
// environment variable this port sets — this project's own hard "no
// secrets in argv" rule (matching real onepassword_info.py's own
// choice to use module.run_command's `data=` stdin parameter for the
// exact same reason).
//
// A fatal error occurs (Result{Failed:true}) if any search term's item
// can't be found or its field can't be located within it, matching
// real onepassword_info's own documented "A fatal error occurs if any
// of the items being searched for cannot be found."
//
// Extra["onepassword"]: dict of {item name: {field name: value}} —
// exactly real onepassword_info's own RETURN shape; a document item's
// entry is {"document": <raw document content>} instead of a field
// name, matching real _parse_field's own documentAttributes branch.
func moduleOnepasswordInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	const mod = "onepassword_info"
	terms, err := opParseSearchTerms(args)
	if err != nil {
		return Result{}, err
	}
	cliPath := argString(args, "cli_path", "op")

	session, res, err := opAssertLoggedIn(ctx, conn, cliPath, args)
	if err != nil {
		return Result{}, err
	}
	if res.Failed {
		return res, nil
	}

	result := map[string]any{}
	for _, term := range terms {
		raw, res, err := opGetItem(ctx, conn, cliPath, session, term.name, term.vault)
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail(fmt.Sprintf("%s: unable to find an item in 1Password named '%s': %s", mod, term.name, opErrMsg(res))), nil
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}
		value, failMsg, err := opParseField(ctx, conn, cliPath, session, raw, term.name, term.field, term.section)
		if err != nil {
			return Result{}, err
		}
		if failMsg != "" {
			return Fail(mod + ": " + failMsg), nil
		}
		if existing, ok := result[term.name].(map[string]any); ok {
			for k, v := range value {
				existing[k] = v
			}
		} else {
			result[term.name] = value
		}
	}

	return Result{}.WithExtra("onepassword", result), nil
}

type opSearchTerm struct {
	name    string
	field   string
	section string
	vault   string
}

// opParseSearchTerms decodes args["search_terms"] — a list of plain
// strings and/or {name, field, section, vault} dicts — matching real
// OnePasswordInfo.parse_search_terms exactly (field defaults to
// "password").
func opParseSearchTerms(args map[string]any) ([]opSearchTerm, error) {
	v, ok := args["search_terms"]
	if !ok {
		return nil, errArg("onepassword_info: missing required argument: search_terms")
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errArg("onepassword_info: argument search_terms must be a list")
	}
	out := make([]opSearchTerm, 0, len(list))
	for _, item := range list {
		t := opSearchTerm{field: "password"}
		switch x := item.(type) {
		case string:
			t.name = x
		case map[string]any:
			name, _ := x["name"].(string)
			if name == "" {
				return nil, errArg("onepassword_info: missing required 'name' field from search term, got: %v", x)
			}
			t.name = name
			if f, ok := x["field"].(string); ok && f != "" {
				t.field = f
			}
			if s, ok := x["section"].(string); ok {
				t.section = s
			}
			if vt, ok := x["vault"].(string); ok {
				t.vault = vt
			}
		default:
			return nil, errArg("onepassword_info: search_terms entries must be a string or a dict, got %T", item)
		}
		out = append(out, t)
	}
	return out, nil
}

// opRun runs `<cliPath> <argv...> [--session=session]`, piping stdin
// (if non-empty) to it — matching real OnePasswordInfo._run exactly.
func opRun(ctx context.Context, conn remoteexec.Connection, cliPath, session string, stdin string, argv ...string) (remoteexec.Result, error) {
	full := append([]string{}, argv...)
	if session != "" {
		full = append(full, "--session="+session)
	}
	quoted := make([]string, len(full)+1)
	quoted[0] = shellQuote(cliPath)
	for i, a := range full {
		quoted[i+1] = shellQuote(a)
	}
	cmd := strings.Join(quoted, " ")
	if stdin != "" {
		return conn.Exec(ctx, cmd, strings.NewReader(stdin))
	}
	return conn.Exec(ctx, cmd, nil)
}

func opErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// opAssertLoggedIn matches real OnePasswordInfo.assert_logged_in: `op
// get account` first; on failure, one signin attempt via auto_login —
// see moduleOnepasswordInfo's own doc comment on how this port's
// signin-branch selection narrows real get_token()'s own
// config-file-existence check.
func opAssertLoggedIn(ctx context.Context, conn remoteexec.Connection, cliPath string, args map[string]any) (session string, res Result, err error) {
	probe, perr := opRun(ctx, conn, cliPath, "", "", "get", "account")
	if perr != nil {
		return "", Result{}, perr
	}
	if probe.RC == 0 {
		return "", Result{}, nil
	}

	autoLogin, _ := args["auto_login"].(map[string]any)
	if autoLogin == nil {
		return "", Fail("onepassword_info: unable to perform an initial sign in to 1Password. Please run " +
			"'op signin' or define credentials in 'auto_login'."), nil
	}
	masterPassword, _ := autoLogin["master_password"].(string)
	if masterPassword == "" {
		return "", Fail("onepassword_info: unable to sign in to 1Password. 'auto_login.master_password' is required."), nil
	}
	subdomain, _ := autoLogin["subdomain"].(string)
	username, _ := autoLogin["username"].(string)
	secretKey, _ := autoLogin["secret_key"].(string)

	var signinRes remoteexec.Result
	if subdomain != "" && username != "" && secretKey != "" {
		signinRes, err = opRun(ctx, conn, cliPath, "", masterPassword,
			"signin", subdomain+".1password.com", username, secretKey, "--output=raw")
	} else if username != "" || secretKey != "" {
		// Real full_login() requires ALL of subdomain/username/secret_key/
		// master_password together for a fresh sign-in.
		return "", Fail("onepassword_info: unable to perform initial sign in to 1Password. subdomain, " +
			"username, secret_key, and master_password are all required to perform initial sign in."), nil
	} else if subdomain != "" {
		signinRes, err = opRun(ctx, conn, cliPath, "", masterPassword, "signin", subdomain, "--output=raw")
	} else {
		signinRes, err = opRun(ctx, conn, cliPath, "", masterPassword, "signin", "--output=raw")
	}
	if err != nil {
		return "", Result{}, err
	}
	if signinRes.RC != 0 {
		return "", Fail("onepassword_info: failed to sign in to 1Password: " + opErrMsg(signinRes)), nil
	}
	return strings.TrimSpace(signinRes.Stdout), Result{}, nil
}

// opGetItem runs `op get item <name> [--vault=vault]` — matching real
// OnePasswordInfo.get_raw.
func opGetItem(ctx context.Context, conn remoteexec.Connection, cliPath, session, name, vault string) (string, remoteexec.Result, error) {
	argv := []string{"get", "item", name}
	if vault != "" {
		argv = append(argv, "--vault="+vault)
	}
	res, err := opRun(ctx, conn, cliPath, session, "", argv...)
	if err != nil {
		return "", res, err
	}
	return res.Stdout, res, nil
}

// opParseField matches real OnePasswordInfo._parse_field: a document
// item fetches its content via `op get document <title>`; otherwise it
// searches details[field], then details.fields[], then
// details.sections[].fields[] (optionally scoped to one section).
func opParseField(ctx context.Context, conn remoteexec.Connection, cliPath, session, rawJSON, itemName, fieldName, sectionTitle string) (value map[string]any, failMsg string, err error) {
	var data struct {
		Overview struct {
			Title string `json:"title"`
		} `json:"overview"`
		Details struct {
			DocumentAttributes json.RawMessage `json:"documentAttributes"`
			Password           *string         `json:"password"`
			Fields             []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
			Sections []struct {
				Title  string `json:"title"`
				Fields []struct {
					T string `json:"t"`
					V string `json:"v"`
				} `json:"fields"`
			} `json:"sections"`
		} `json:"details"`
	}
	if jerr := json.Unmarshal([]byte(rawJSON), &data); jerr != nil {
		return nil, "", fmt.Errorf("decoding op get item response for %q: %w", itemName, jerr)
	}

	if len(data.Details.DocumentAttributes) > 0 {
		title := data.Overview.Title
		if title == "" {
			title = itemName
		}
		docRes, derr := opRun(ctx, conn, cliPath, session, "", "get", "document", title)
		if derr != nil {
			return nil, "", derr
		}
		if docRes.RC != 0 {
			return nil, "", fmt.Errorf("op get document %q: %s", title, opErrMsg(docRes))
		}
		return map[string]any{"document": strings.TrimSpace(docRes.Stdout)}, "", nil
	}

	if fieldName == "password" && data.Details.Password != nil {
		return map[string]any{fieldName: *data.Details.Password}, "", nil
	}

	if sectionTitle == "" {
		for _, f := range data.Details.Fields {
			if strings.EqualFold(f.Name, fieldName) {
				return map[string]any{fieldName: f.Value}, "", nil
			}
		}
	}
	for _, sec := range data.Details.Sections {
		if sectionTitle != "" && !strings.EqualFold(sec.Title, sectionTitle) {
			continue
		}
		for _, f := range sec.Fields {
			if strings.EqualFold(f.T, fieldName) {
				return map[string]any{fieldName: f.V}, "", nil
			}
		}
	}

	optionalSection := ""
	if sectionTitle != "" {
		optionalSection = fmt.Sprintf(" in the section '%s'", sectionTitle)
	}
	return nil, fmt.Sprintf("unable to find an item in 1Password named '%s' with the field '%s'%s.", itemName, fieldName, optionalSection), nil
}
