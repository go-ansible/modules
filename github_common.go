package modules

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the nine github_*.go modules in this
// batch share: shelling out to the `gh` CLI (GitHub's own official
// command-line tool) instead of talking to the GitHub REST API through
// PyGithub or github3.py the way every real github_* (community.general)
// module does. This is the same "shell out to the platform's own
// official CLI instead of an API client" precedent this port already
// uses for Consul (consul_kv.go/consul_session.go), Redis (redis.go),
// Terraform (terraform.go), Icinga2, and Kopia — a deliberate,
// user-approved architectural decision for this batch, not a gap.
//
// # Auth precondition
//
// `gh` must already be authenticated on the TARGET host before any
// github_* module in this port runs: either `gh auth login` has
// already been completed there, or GH_TOKEN/GITHUB_TOKEN is already
// set in that session's own environment. This port does not attempt
// to drive `gh auth login` itself (an interactive OAuth device-code
// flow, or a token write to `gh`'s own credential store) from a
// module — matching the precedent ipa_common.go's own doc comment
// already sets for ipa_*'s pre-existing-Kerberos-ticket precondition:
// a module here composes portable POSIX shell commands run via a
// Connection (see module.go's own doc comment); driving an
// interactive login ceremony is out of scope for all of them.
//
// # Auth arguments
//
// Every real github_* module in this batch documents its own
// username/password/token/otp-style auth arguments (the exact set
// varies per module — see each module<Name> function's own doc
// comment for which it has). `gh` itself has no per-invocation
// username/password/OTP concept at all — those are github3.py's/
// PyGithub's own basic-auth and 2FA-challenge-response login flows,
// with no equivalent in `gh`, which authenticates only via a token
// (supplied per-invocation through GH_TOKEN, or read from its own
// pre-stored OAuth/keyring credential when GH_TOKEN is unset). So:
//   - every github_* module here still ACCEPTS its own real
//     username/password/otp arguments, for argument-shape
//     compatibility with real playbooks written against the real
//     module, but they have NO EFFECT on this port's behavior.
//   - a `token`-shaped argument (present on the real module — again,
//     varies per module) IS wired in: this port exports it as
//     GH_TOKEN for that single `gh` invocation only, NEVER as a
//     `--token`/`-t` command-line flag — this project's own hard
//     "no secrets in argv" rule (see redis.go's own REDISCLI_AUTH
//     precedent, and consul_kv.go's CONSUL_HTTP_TOKEN one).
//   - when no token argument is given (or a module's real counterpart
//     has none at all, e.g. github_deploy_key/github_release/
//     github_webhook* which also accept username/password), `gh`
//     falls back to its own already-authenticated session as-is.
//
// No github_* module in this batch invents a token/username/password
// argument that its real counterpart does not document — see each
// module's own doc comment for its exact accepted set.

// ghCmd renders one `gh` invocation, shell-quoting each argv entry.
func ghCmd(argv ...string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return "gh " + strings.Join(quoted, " ")
}

// ghRun runs one `gh` invocation on conn, passing token (if non-empty)
// via the GH_TOKEN environment variable for that single command only —
// see this file's own doc comment for why, never as a command-line
// flag. stdin, if non-nil, is piped to the command — used by
// github_deploy_key.go/github_key.go's own "-" key-file argument, so
// an SSH public key string never has to touch a temp file on the
// target at all.
func ghRun(ctx context.Context, conn remoteexec.Connection, token string, stdin io.Reader, argv ...string) (remoteexec.Result, error) {
	cmd := ghCmd(argv...)
	if token != "" {
		cmd = "GH_TOKEN=" + shellQuote(token) + " " + cmd
	}
	return conn.Exec(ctx, cmd, stdin)
}

// ghRunJSON runs argv (via ghRun) and decodes its stdout as JSON into
// out. Returns the raw Result too, so a caller can still inspect
// RC/Stderr on failure.
func ghRunJSON(ctx context.Context, conn remoteexec.Connection, token string, out any, argv ...string) (remoteexec.Result, error) {
	res, err := ghRun(ctx, conn, token, nil, argv...)
	if err != nil {
		return res, err
	}
	if res.RC != 0 {
		return res, nil
	}
	if strings.TrimSpace(res.Stdout) == "" {
		return res, nil
	}
	if jerr := json.Unmarshal([]byte(res.Stdout), out); jerr != nil {
		return res, jerr
	}
	return res, nil
}

// ghStderr prefers a `gh` invocation's own stderr for an error message,
// falling back to stdout (some `gh` failures — e.g. an interactive
// prompt refused because stdin isn't a terminal — print to stdout).
func ghStderr(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// ghNotFound reports whether a failed `gh` invocation's own error text
// looks like a 404 — `gh`'s own HTTP-error wrapping consistently
// renders a REST 404 as literal text containing "HTTP 404" (verified
// directly against this batch's own locally installed `gh` binary,
// e.g. `gh ssh-key delete <bad-id>` -> "HTTP 404: Not Found (...)"),
// which every github_*.go module in this batch relies on to
// distinguish "the thing doesn't exist" (Changed=false, not a
// failure) from a genuine error.
func ghNotFound(res remoteexec.Result) bool {
	return strings.Contains(ghStderr(res), "HTTP 404")
}
