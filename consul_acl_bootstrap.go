package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleConsulACLBootstrap implements Ansible's `consul_acl_bootstrap`
// (community.general) module: bootstraps a Consul cluster's ACL system
// via the `consul` CLI's own `consul acl bootstrap` subcommand — see
// consul_acl.go's own consulACLRun doc comment for why this port
// substitutes the CLI for real consul_acl_bootstrap's python-consul/
// requests HTTP client.
//
// Args: bootstrap_secret (a UUID to use as the initial management
// token's secret ID) — per `consul acl bootstrap`'s own CLI reference,
// this is supplied as a FILE argument (or "-" for stdin) whose content
// is read as the secret, not a flag, so this port writes it to a
// target-side temp file via conn.TempPath and passes that path,
// matching kdeconfig.go's own temp-file-then-cleanup shape; state
// (present|bootstrapped, default present — both values request the same
// one-shot bootstrap operation, matching real consul_acl_bootstrap's
// own choices, which do not otherwise branch behavior); host (default
// localhost); port (default 8500); scheme (default http); datacenter;
// ca_path; validate_certs (default true). No `token` argument — real
// consul_acl_bootstrap takes none either, since bootstrapping is what
// creates the first token.
//
// ACL bootstrap can only ever succeed once per cluster; a second attempt
// returns a Consul error rather than the same result idempotently. This
// port treats a non-zero exit whose stderr mentions "already" or "no
// longer allowed" (case-insensitive) as an already-bootstrapped cluster
// — Ok (unchanged), not Fail — matching real consul_acl_bootstrap's own
// documented handling of that specific, expected error; any other
// non-zero exit is a genuine Fail.
//
// Extra["result"]: the bootstrap response's own AccessorID/SecretID/...
// fields, matching real consul_acl_bootstrap's own `result` return
// value, present only when Changed (nil otherwise, since an
// already-bootstrapped cluster gives this port no token to report).
func moduleConsulACLBootstrap(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	state := argString(args, "state", "present")
	if state != "present" && state != "bootstrapped" {
		return Result{}, errArg("consul_acl_bootstrap: state must be present or bootstrapped, got %q", state)
	}

	opts := append([]string{}, consulConnArgs(args)...)
	opts = append(opts, "-format=json")

	var fileArg string
	if secret := argString(args, "bootstrap_secret", ""); secret != "" {
		fileArg = conn.TempPath("consul-bootstrap-secret")
		if _, err := conn.Exec(ctx, "cat > "+shellQuote(fileArg), strings.NewReader(secret)); err != nil {
			return Result{}, err
		}
		defer func() { _ = conn.Remove(ctx, fileArg) }()
	}

	all := append([]string{"acl", "bootstrap"}, opts...)
	if fileArg != "" {
		all = append(all, fileArg)
	}
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = shellQuote(a)
	}
	res, err := conn.Exec(ctx, "consul "+strings.Join(quoted, " "), nil)
	if err != nil {
		return Result{}, err
	}

	if res.RC != 0 {
		stderr := strings.ToLower(res.Stderr)
		if strings.Contains(stderr, "already") || strings.Contains(stderr, "no longer allowed") {
			return Ok("consul_acl_bootstrap: ACL system already bootstrapped").WithExtra("result", nil), nil
		}
		return Fail("consul_acl_bootstrap: " + strings.TrimSpace(res.Stderr)), nil
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(res.Stdout), &result); err != nil {
		return Result{}, fmt.Errorf("consul_acl_bootstrap: decoding JSON output: %w", err)
	}
	return Changed("consul_acl_bootstrap: ACL system bootstrapped").WithExtra("result", result), nil
}
