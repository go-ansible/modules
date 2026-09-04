package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// modulePritunlUser implements Ansible's `pritunl_user`
// (community.general) module: creates, updates, or deletes a Pritunl
// user within an organization. This port cannot implement it — see
// pritunl_common.go's own doc comment for exactly what was checked
// (Pritunl's own official `pritunl` CLI has no organization/user
// management subcommand at all) and why this is a confirmed gap, not a
// guess.
//
// Real args (documented for reference only — never parsed, since this
// module fails before touching any of them): organization (aliases
// org, required) — the organization the user belongs to; user_name
// (required); user_email; user_type (client|server, default client);
// user_groups ([]string); user_disabled (bool); user_gravatar (bool);
// user_mac_addresses ([]string); state (present|absent, default
// present); pritunl_url/pritunl_api_token/pritunl_api_secret —
// Pritunl's own signed-REST-API connection details; validate_certs
// (bool, default true).
//
// Every state Fails loud (Result{Failed:true}), per this batch's own
// explicit instructions for exactly this situation: fail loud rather
// than silently fake parity when a real capability genuinely has no
// CLI equivalent.
func modulePritunlUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return pritunlNoCLISupport("pritunl_user"), nil
}
