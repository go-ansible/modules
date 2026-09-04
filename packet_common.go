package modules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what the six packet_*.go modules in this batch
// (packet_device, packet_ip_subnet, packet_project, packet_sshkey,
// packet_volume, packet_volume_attachment) share: shelling out to
// Equinix Metal's own official CLI, `metal`, instead of talking to
// packet.net's REST API through the `packet-python` SDK the way every
// real packet_* module in this batch does.
//
// # `metal`, not `packet-cli` — verified, and why
//
// Packet.net was acquired by Equinix and rebranded Equinix Metal; its
// modern official CLI is `metal` (github.com/equinix/metal-cli,
// docs.equinix.com/metal/libraries/cli/ — both confirmed via this
// batch's own research), the SUCCESSOR to the older, now-archived
// `packet-cli`. This port targets `metal`, not `packet-cli`, because
// `metal` is what Equinix Metal documents as current today; every
// packet_* module's own real Python source still talks to the OLD
// packet.net API surface (module names, auth_token env var
// PACKET_API_TOKEN, facility-centric device creation), which this
// port's own doc comments flag wherever `metal`'s modern shape
// (METAL_AUTH_TOKEN, metro alongside facility, ...) diverges from it.
//
// # Auth precondition
//
// `metal` must already be configured on the target before any
// packet_* module in this port runs — either via a prior `metal init`
// (which writes ~/.config/equinix/metal.yaml, per metal-cli's own
// docs) or the METAL_AUTH_TOKEN environment variable already set in
// the invoking session. This port does not manage that configuration
// itself, matching every other CLI-substitution module in this
// project (ipa_common.go's kinit precondition, github_common.go's
// GH_TOKEN-or-already-authenticated-`gh` precondent).
//
// Every real packet_* module's own `auth_token` argument (accepted for
// argument-shape compatibility, documented per-module) IS wired
// through — as the METAL_AUTH_TOKEN environment variable for that
// single invocation only, never as `metal`'s own `--token` flag,
// matching this project's own hard "no secrets in argv" rule (see
// redis.go's own REDISCLI_AUTH precedent). `metal`'s own env var is
// METAL_AUTH_TOKEN, NOT the older packet-cli/packet.net
// PACKET_API_TOKEN name real packet_*.py's own doc text mentions —
// this port uses the CLI's actual current env var, confirmed from
// metal-cli's own generated command docs.
//
// # Output format
//
// Every metalRun call in this file passes `-o json` (one of metal-cli's
// own documented output formats — table/json/yaml/sh/terraform/capp),
// confirmed from metal-cli's own generated per-command docs, so this
// port can decode structured results rather than parsing table output.
//
// # A real, confirmed gap: no volume/storage support at all
//
// Equinix Metal's own classic Elastic Block Storage ("volumes")
// product — what real packet_volume.py/packet_volume_attachment.py
// both manage — has NO subcommand anywhere in `metal`'s own command
// tree (confirmed: this batch's own research fetched metal-cli's own
// full docs/ directory listing and found device/ip/ssh-key/project/
// gateway/vrf/... subcommands, but no metal_volume.md or
// metal_storage.md of any kind), and `metal` has no generic
// API-passthrough subcommand either (no `metal api ...` the way `gh
// api`/`glab api` provide as a fallback in this batch's sibling
// GitHub/GitLab modules) — confirmed from metal-cli's own README,
// which documents no such command. packet_volume.go and
// packet_volume_attachment.go therefore FAIL LOUD
// (Result{Failed:true}) for every state, honestly documenting this as
// a genuine capability gap in Equinix's own modern CLI rather than
// faking parity or silently no-op'ing — per this batch's own explicit
// instructions for exactly this situation.
//
// metalRequireBinary fails cleanly (Result{Failed:true}, not a Go
// error) if the real `metal` CLI is not on the target's PATH.
func metalRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, "command -v metal"); err != nil {
		return Fail(fmt.Sprintf("%s: the metal binary (Equinix Metal's own current CLI, github.com/equinix/"+
			"metal-cli — the successor to the older packet-cli) is required on the target and was not found "+
			"in PATH — this port shells out to it rather than speaking packet.net's REST API via the "+
			"packet-python SDK directly; see packet_common.go's own doc comment, including the precondition "+
			"that `metal init` must already have been run (or METAL_AUTH_TOKEN already set) on the target", moduleName)), false
	}
	return Result{}, true
}

// metalCmd renders one `metal <argv...> -o json [--token=TOKEN via env,
// never here]` invocation, shell-quoting each argv entry.
func metalCmd(argv ...string) string {
	full := append([]string{"metal"}, argv...)
	full = append(full, "-o", "json")
	quoted := make([]string, len(full))
	for i, a := range full {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// metalRun runs one `metal` invocation on conn, passing authToken (if
// non-empty) via the METAL_AUTH_TOKEN environment variable for that
// single command only — see this file's own doc comment for why,
// never as a --token flag.
func metalRun(ctx context.Context, conn remoteexec.Connection, authToken string, argv ...string) (remoteexec.Result, error) {
	cmd := metalCmd(argv...)
	if authToken != "" {
		cmd = "METAL_AUTH_TOKEN=" + shellQuote(authToken) + " " + cmd
	}
	return conn.Exec(ctx, cmd, nil)
}

// metalRunJSON runs argv (via metalRun) and decodes its stdout as JSON
// into out on success. A non-zero RC is returned as-is (out left
// untouched) for the caller to interpret.
func metalRunJSON(ctx context.Context, conn remoteexec.Connection, authToken string, out any, argv ...string) (remoteexec.Result, error) {
	res, err := metalRun(ctx, conn, authToken, argv...)
	if err != nil {
		return res, err
	}
	if res.RC != 0 || out == nil || strings.TrimSpace(res.Stdout) == "" {
		return res, nil
	}
	if jerr := json.Unmarshal([]byte(res.Stdout), out); jerr != nil {
		return res, fmt.Errorf("decoding metal %s output: %w", strings.Join(argv, " "), jerr)
	}
	return res, nil
}

// metalErrMsg builds a Fail() message body from a non-zero metal
// result, preferring stderr but falling back to stdout.
func metalErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}

// metalFail builds a Fail() Result for one failed metal invocation.
func metalFail(moduleName, action string, res remoteexec.Result) Result {
	return Fail(fmt.Sprintf("%s: %s: %s", moduleName, action, metalErrMsg(res)))
}

// metalNoVolumeSupport is shared by packet_volume.go and
// packet_volume_attachment.go — see this file's own doc comment on
// this confirmed, real gap in `metal`'s own command tree.
func metalNoVolumeSupport(moduleName string) Result {
	return Fail(fmt.Sprintf("%s: not supported by this port — Equinix Metal's own current CLI (`metal`) has "+
		"no volume/storage subcommand anywhere in its command tree, and no generic API-passthrough fallback "+
		"either (confirmed from metal-cli's own docs/ directory and README); real %s.py manages Equinix "+
		"Metal's classic Elastic Block Storage product via packet-python directly, which this port has no "+
		"CLI it could shell out to for this specific resource — see packet_common.go's own doc comment", moduleName, moduleName))
}

// metalFindByField filters items (each a decoded JSON object) to those
// whose field equals want (via fmt.Sprint string comparison). Returns
// the single match, or ok=false if zero matched, or ambiguous=true if
// more than one did.
func metalFindByField(items []map[string]any, field, want string) (item map[string]any, ok bool, ambiguous bool) {
	var matches []map[string]any
	for _, it := range items {
		if fmt.Sprint(it[field]) == want {
			matches = append(matches, it)
		}
	}
	if len(matches) == 0 {
		return nil, false, false
	}
	if len(matches) > 1 {
		return nil, false, true
	}
	return matches[0], true, false
}

// metalListArray finds the one array-valued top-level field in a
// decoded metal-cli list-style JSON response (metal-cli's own `-o
// json` list output nests the resource array under a plural field
// alongside pagination metadata) — see hwc_common.go's own
// hcloudListArray for the identical rationale, shared verbatim here
// under a differently-named function since this file's own doc
// comment is specific to `metal`, not KooCLI.
func metalListArray(raw map[string]any) []map[string]any {
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if arr, ok := raw[k].([]any); ok {
			out := make([]map[string]any, 0, len(arr))
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					out = append(out, m)
				}
			}
			return out
		}
	}
	return nil
}
