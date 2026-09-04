package modules

import (
	"context"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleLinode implements Ansible's `linode` (community.general)
// module — the OLDER of this batch's two Linode modules. Real
// linode.py, read before implementing, manages Linode instances
// through Linode's own "Classic"/v3 API shape (numeric
// datacenter/distribution/plan/kernel_id arguments, an `api_key`
// argument, LINODE_API_KEY env var) — the module's own doc text even
// says so: "linode_v4... aims to replace the current Linode module
// which uses deprecated API bindings on the Linode side." linode.py
// and linode_v4.py are NOT duplicates: they target genuinely different
// API generations with genuinely different argument shapes (numeric
// IDs here vs. string slugs/regions in linode_v4.py) — see
// linode_v4.go's own doc comment for that module.
//
// This port cannot implement linode.py at all, for a reason stronger
// than "no CLI covers it": Linode's own v3 API — the one, specific
// API this module's real Python source talks to — was permanently
// retired. Linode announced v3's deprecation in 2018 alongside v4's
// release, and completed its full shutdown on 2023-07-31 (confirmed
// via multiple independent sources during this batch's own research,
// including Linode's own community forum and the Salt project's own
// "[TECH DEBT] Linode APIv3 Reaching End of Life on July 31, 2023"
// tracking issue) — v3 API calls have returned nothing but errors
// since that date. `linode-cli` (this batch's sibling linode_v4.go's
// own CLI substitution) only ever targeted v4; it has no v3 mode to
// fall back to, because there is no longer a v3 API anywhere to talk
// to, official CLI or otherwise. This is not a gap in this port's own
// architecture or in Linode's CLI tooling specifically — it is a fact
// about the target platform itself: even the REAL upstream
// linode_api4-based linode.py module, run today, unmodified, against
// current Linode, would itself fail on every single call. Every state
// therefore Fails loud (Result{Failed:true}), matching this project's
// own synchronize.go precedent for "genuinely cannot be done," rather
// than silently no-op'ing or attempting some plausible-looking
// approximation against an API that no longer exists.
func moduleLinode(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	return Fail("linode: not supported by this port — Linode's own v3 (\"Classic\") API, the ONE API real " +
		"linode.py's own source talks to, was permanently retired by Linode itself on 2023-07-31; there is no " +
		"longer any API (official CLI or otherwise) for this module, or the real upstream module it ports, to " +
		"call — see linode.go's own doc comment for the sources checked. Use linode_v4 instead, which targets " +
		"Linode's still-live v4 API via linode-cli"), nil
}
