package modules

import (
	"context"
	"io"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what gitlab_deploy_key.go, gitlab_issue.go, and
// gitlab_merge_request.go share: driving `glab`'s own dedicated
// `deploy-key`/`issue`/`mr` subcommands directly, rather than `glab
// api` — this batch's own default fallback, see gitlab_common.go's own
// doc comment — because those three subcommand families exist, are
// documented, and cover this port's needs, matching this batch's own
// explicit instruction to prefer a dedicated `glab` subcommand over
// `glab api` whenever one genuinely does. Every OTHER gitlab_* module
// in this batch (gitlab_branch, gitlab_group*, gitlab_hook,
// gitlab_label, gitlab_*_variable) uses `glab api` instead — either
// because `glab` has no dedicated subcommand for that resource at all,
// or (gitlab_label) because the dedicated subcommand's own group-scope
// support is inconsistent across its own create/list/delete
// subcommands (verified against docs.gitlab.com/cli/label/*, not
// guessed) — see each of those modules' own doc comment for why it
// chose `glab api` instead.
//
// Like every other gitlab_* module in this batch, none of these three
// resolve `glab`'s own auth from this port's api_token/api_url/...
// arguments — see gitlab_common.go's own doc comment for the
// documented, accepted-but-inert narrowing and the `glab auth login`/
// GITLAB_TOKEN precondition.

// glabCLI runs "glab <argv...>" (optionally piping stdin — e.g. a
// public key or a token value glab reads from a "-" positional/flag
// rather than accept on argv, keeping it out of the target's own
// process listing; see redis.go's own REDISCLI_AUTH doc comment for why
// this port avoids putting a secret on argv wherever the wrapped CLI
// offers an alternative) and returns its raw result — RC not treated as
// an error, callers decide what a non-zero exit means.
func glabCLI(ctx context.Context, conn remoteexec.Connection, stdin io.Reader, argv ...string) (remoteexec.Result, error) {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return conn.Exec(ctx, strings.Join(quoted, " "), stdin)
}
