package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleNomadToken implements Ansible's `nomad_token`
// (community.general) module: creates, updates, or deletes a Nomad ACL
// token (or bootstraps the cluster's very first management token) via
// the `nomad` CLI — see nomad_job.go's own nomadConnArgs doc comment
// for why this port substitutes the CLI for real nomad_token's
// python-nomad HTTP API client.
//
// Args: host (required); port/use_ssl/validate_certs/client_cert/
// client_key/namespace/token/timeout — see nomad_job.go's own
// nomadConnArgs; state (present|absent, required); name — required
// for state=absent, and for state=present unless token_type=bootstrap;
// token_type (client|management|bootstrap, default "client");
// policies ([]string); global_replicated (bool, default false) —
// mapped to `-global` on create/update.
//
// token_type=bootstrap: runs `nomad acl bootstrap -json` unconditionally,
// this port has no cheap way to distinguish "already bootstrapped" from
// any other failure except by matching the CLI's own stderr text ("ACL
// bootstrap already done" — Nomad's own fixed error string for this
// specific case since Nomad 0.7), so a bootstrap attempt against an
// already-bootstrapped cluster is reported Ok (unchanged), not Failed;
// this is textual pattern matching against a real Nomad error message
// rather than a structured signal, and could misclassify if the string
// ever changes upstream.
//
// token_type=client/management: looks up an existing token by `name`
// via `nomad acl token list -json` (real Nomad ACL tokens have no
// natural-key uniqueness on name — python-nomad's own module resolves
// this the same way, scanning the list for a name match and using
// whichever one it finds first). state=present: if no match, creates
// via `nomad acl token create -json`; if a match exists and its own
// type/policies/global fields already equal the desired values,
// reports unchanged; otherwise updates via `nomad acl token update
// -json <accessorID>` (Nomad's own PUT-like semantics: fields omitted
// from the update call are left as they were, so this port always
// passes name/type/policies/global explicitly rather than relying on
// unspecified-field preservation). state=absent: deletes the matched
// token via `nomad acl token delete <accessorID>`, or reports
// unchanged if no token with that name exists.
//
// Extra["result"]: the created/updated token's own JSON object (with
// SecretID present only right after creation, matching real Nomad's
// own one-time-reveal semantics — an update or a lookup-only path
// never re-exposes it), matching real nomad_token's own documented
// `result` return shape.
func moduleNomadToken(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	if _, err := requireString(args, "host"); err != nil {
		return Result{}, err
	}
	state, err := requireString(args, "state")
	if err != nil {
		return Result{}, err
	}
	if state != "present" && state != "absent" {
		return Result{}, errArg("nomad_token: state must be present or absent, got %q", state)
	}
	tokenType := argString(args, "token_type", "client")

	if tokenType == "bootstrap" {
		if state != "present" {
			return Result{}, errArg("nomad_token: token_type=bootstrap only supports state=present")
		}
		argv := append([]string{"nomad", "acl", "bootstrap", "-json"}, nomadConnArgs(args)...)
		res, err := nomadRun(ctx, conn, args, argv, "")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			if strings.Contains(res.Stderr, "already") {
				return Ok("ACL already bootstrapped"), nil
			}
			return Fail("nomad_token: bootstrap: " + strings.TrimSpace(res.Stderr)), nil
		}
		var result map[string]any
		_ = json.Unmarshal([]byte(res.Stdout), &result)
		return Changed("bootstrapped").WithExtra("result", result), nil
	}

	name := argString(args, "name", "")
	if name == "" {
		return Result{}, errArg("nomad_token: name is required unless token_type=bootstrap")
	}
	existing, err := nomadFindTokenByName(ctx, conn, args, name)
	if err != nil {
		return Result{}, err
	}

	if state == "absent" {
		if existing == nil {
			return Ok(name + " already absent"), nil
		}
		accessor, _ := (*existing)["AccessorID"].(string)
		argv := append([]string{"nomad", "acl", "token", "delete", accessor}, nomadConnArgs(args)...)
		res, err := nomadRun(ctx, conn, args, argv, "")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("nomad_token: deleting " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(name + " deleted"), nil
	}

	policies := argStringList(args, "policies")
	global := argBool(args, "global_replicated", false)

	if existing == nil {
		argv := []string{"nomad", "acl", "token", "create", "-name=" + name, "-type=" + tokenType}
		for _, p := range policies {
			argv = append(argv, "-policy="+p)
		}
		argv = append(argv, "-global="+boolStr(global), "-json")
		argv = append(argv, nomadConnArgs(args)...)
		res, err := nomadRun(ctx, conn, args, argv, "")
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("nomad_token: creating " + name + ": " + strings.TrimSpace(res.Stderr)), nil
		}
		var result map[string]any
		_ = json.Unmarshal([]byte(res.Stdout), &result)
		return Changed(name+" created").WithExtra("result", result), nil
	}

	existingType, _ := (*existing)["Type"].(string)
	existingGlobal, _ := (*existing)["Global"].(bool)
	var existingPolicies []string
	if raw, ok := (*existing)["Policies"].([]any); ok {
		for _, p := range raw {
			existingPolicies = append(existingPolicies, fmt.Sprint(p))
		}
	}
	sort.Strings(existingPolicies)
	wantPolicies := append([]string(nil), policies...)
	sort.Strings(wantPolicies)

	if existingType == tokenType && existingGlobal == global && stringSlicesEqual(existingPolicies, wantPolicies) {
		return Ok(name+" already up to date").WithExtra("result", *existing), nil
	}

	accessor, _ := (*existing)["AccessorID"].(string)
	argv := []string{"nomad", "acl", "token", "update", "-name=" + name, "-type=" + tokenType}
	for _, p := range policies {
		argv = append(argv, "-policy="+p)
	}
	argv = append(argv, "-global="+boolStr(global), "-json", accessor)
	argv = append(argv, nomadConnArgs(args)...)
	res, err := nomadRun(ctx, conn, args, argv, "")
	if err != nil {
		return Result{}, err
	}
	if res.RC != 0 {
		return Fail("nomad_token: updating " + name + ": " + strings.TrimSpace(res.Stderr)), nil
	}
	var result map[string]any
	_ = json.Unmarshal([]byte(res.Stdout), &result)
	return Changed(name+" updated").WithExtra("result", result), nil
}

// nomadFindTokenByName runs `nomad acl token list -json` and returns
// the first entry whose "Name" matches, or nil if none do.
func nomadFindTokenByName(ctx context.Context, conn remoteexec.Connection, args map[string]any, name string) (*map[string]any, error) {
	argv := append([]string{"nomad", "acl", "token", "list", "-json"}, nomadConnArgs(args)...)
	res, err := nomadRun(ctx, conn, args, argv, "")
	if err != nil {
		return nil, err
	}
	if res.RC != 0 {
		return nil, fmt.Errorf("nomad_token: listing tokens: %s", strings.TrimSpace(res.Stderr))
	}
	var tokens []map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &tokens); err != nil {
		return nil, fmt.Errorf("nomad_token: parsing nomad acl token list output: %w", err)
	}
	for _, tok := range tokens {
		if n, _ := tok["Name"].(string); n == name {
			t := tok
			return &t, nil
		}
	}
	return nil, nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
