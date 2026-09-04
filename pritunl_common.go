package modules

import "fmt"

// This file factors out the fail-loud stance shared by the four
// pritunl_*.go modules in this batch (pritunl_org, pritunl_org_info,
// pritunl_user, pritunl_user_info).
//
// # Investigated, and confirmed: no usable official CLI for this
//
// Real pritunl_org.py/pritunl_org_info.py/pritunl_user.py/
// pritunl_user_info.py all talk directly to Pritunl's own REST API
// using a signed-request auth scheme (an HMAC computed from
// pritunl_api_token/pritunl_api_secret, a nonce, and a timestamp,
// per module_utils' own Pritunl API client) — there is no official
// Python SDK either, just hand-rolled signed `requests` calls.
//
// Pritunl's server package does ship an official `pritunl` CLI
// (installed alongside pritunl-server on the server host itself), but
// this batch's own research (fetched docs.pritunl.com/docs/commands,
// Pritunl's own published command reference) confirms its FULL
// subcommand set is: start, version, reset-password, reset-version,
// reset-ssl-cert, reconfigure, set-mongodb, logs, set, unset, get —
// every one of these is a SERVER-LIFECYCLE or local-configuration
// operation (launching the daemon, resetting the admin's own
// credentials, pointing it at a different MongoDB, reading/writing its
// own internal advanced-config key/value store). Not one of them is an
// organization or user management operation: there is no `pritunl org
// ...`/`pritunl user ...` subcommand family, and no generic
// API-passthrough fallback either (unlike `gh api`/`glab api`/`scw
// ... -o json` in this batch's sibling CLI-substitution modules) —
// confirmed from that same docs page's own full command listing.
//
// (`pritunl-client` is a completely different tool — the end-user
// OpenVPN/WireGuard CLIENT used to CONNECT to a Pritunl VPN profile —
// and was checked and ruled out too, not confused with the server's own
// `pritunl` CLI: it has no server-administration API surface at all,
// let alone org/user CRUD.)
//
// This is a confirmed, real gap in the target platform's own tooling,
// not a guess — and not something this port papers over by
// hand-rolling its own curl-plus-HMAC-signing REST client, which would
// only silently reinvent real pritunl_org.py's/pritunl_user.py's own
// signed-request logic unverified against a live Pritunl server in this
// sandbox (this batch's own explicit instruction: fail loud rather than
// fake parity via improvised curl calls). Every pritunl_*.go module in
// this batch therefore FAILS LOUD (Result{Failed:true}) for every
// state, matching this project's own established precedent for exactly
// this situation — see packet_common.go's own metalNoVolumeSupport and
// packet_volume.go's own "return Fail immediately, no args even parsed"
// shape, which this file's own pritunlNoCLISupport mirrors directly.

// pritunlNoCLISupport builds the Fail() Result every pritunl_*.go
// module in this batch returns unconditionally — see this file's own
// doc comment for exactly what was checked and why.
func pritunlNoCLISupport(moduleName string) Result {
	return Fail(fmt.Sprintf("%s: not supported by this port — Pritunl's own official `pritunl` CLI (installed "+
		"on the Pritunl server itself) has no organization/user management subcommand of any kind (confirmed "+
		"from docs.pritunl.com/docs/commands: only start/version/reset-password/reset-version/reset-ssl-cert/"+
		"reconfigure/set-mongodb/logs/set/unset/get, all server-lifecycle/local-config operations) and no "+
		"generic API-passthrough fallback either; real %s.py talks directly to Pritunl's own signed REST API "+
		"(module_utils' Pritunl API client), which this port has no verified CLI it could shell out to for "+
		"this resource — see pritunl_common.go's own doc comment", moduleName, moduleName))
}
