package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePritunlOrgInfo implements Ansible's `pritunl_org_info`
// (community.general) module: lists Pritunl organizations, optionally
// filtered by name. This port cannot implement it — see
// pritunl_common.go's own doc comment for exactly what was checked
// (Pritunl's own official `pritunl` CLI has no organization/user
// management subcommand at all) and why this is a confirmed gap, not a
// guess.
//
// Real args (documented for reference only — never parsed, since this
// module fails before touching any of them): organization (aliases
// org, optional) — when unset, every organization is returned;
// pritunl_url/pritunl_api_token/pritunl_api_secret — Pritunl's own
// signed-REST-API connection details; validate_certs (bool, default
// true).
//
// Fails loud (Result{Failed:true}), per this batch's own explicit
// instructions for exactly this situation: fail loud rather than
// silently fake parity when a real capability genuinely has no CLI
// equivalent.
func modulePritunlOrgInfo(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return pritunlNoCLISupport("pritunl_org_info"), nil
}
