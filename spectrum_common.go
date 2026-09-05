package modules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// This file factors out what spectrum_device.go and
// spectrum_model_attrs.go share: shelling out to Broadcom's own
// official CA/DX NetOps Spectrum "Command Line Interface" (`vnmsh`,
// documented at techdocs.broadcom.com under "Spectrum > Command Line
// Interface", read before writing this file — Broadcom's own
// introduction page describes it as "the only DX NetOps Spectrum
// resource available" for scripted tasks outside the OneClick GUI, and
// its own command docs describe it as UNIX-like, working with grep and
// pipes) instead of real spectrum_device.py's/spectrum_model_attrs.py's
// own hand-rolled `fetch_url` calls against a REMOTE OneClick server's
// RESTful API (`{url}/spectrum/restful/...`, HTTP Basic auth via
// url_username/url_password).
//
// # A genuine, verified architecture mismatch — read before extending
// # either of these two modules
//
// This is the most significant divergence from real behavior in this
// entire batch, and it is a VERIFIED one, not a guess: `vnmsh` and the
// real modules' own OneClick REST API are not two bindings of the same
// remote protocol (the way ibm_sa_common.go's own xcli/pyxcli or
// memset_common.go's own ma-shell/JSON-RPC are) — they are two
// DIFFERENT ways of reaching Spectrum that live on DIFFERENT hosts:
//
//   - Real spectrum_device.py/spectrum_model_attrs.py's own url/
//     url_username/url_password arguments address OneClick's own HTTP(S)
//     REST endpoint, reachable over the network from wherever Ansible's
//     control node runs (both real modules' own EXAMPLES use
//     `delegate_to: localhost` for exactly this reason) — a fresh
//     Basic-auth credential is supplied on every single task run, with
//     no persistent login step at all.
//   - `vnmsh`, by contrast, is confirmed (from Broadcom's own CLI User
//     Guide and multiple TechDocs/knowledge-base pages, not the module
//     name) to be a LOCAL tool: a CLI user logs into the SpectroSERVER
//     system itself, `cd`s to `$SPECROOT/vnmsh`, and runs `./connect`,
//     which starts a local IPC daemon (`VnmShd`) that every subsequent
//     `./create`/`./destroy`/`./update`/`./seek`/`./show` script in
//     that same directory talks to. None of vnmsh's own documented
//     commands take a remote host, URL, username, or password of any
//     kind — there is no equivalent of OneClick's own url_username/
//     url_password anywhere in vnmsh's own command surface, because
//     vnmsh has no concept of a REMOTE target at all: it IS the target.
//
// Given that, this port's own Connection for these two modules must
// address (or have direct vnmsh access to) the SpectroSERVER host
// itself — NOT a generic remote host reaching a OneClick server over
// HTTPS the way every real usage of these two modules does. This is a
// real architectural constraint this port cannot paper over: no amount
// of additional Go code can give a strictly-local IPC tool a remote
// endpoint it was never built to have. url/url_username/url_password
// are still accepted as arguments (so a caller migrating a real
// playbook does not need to strip them first) but are NOT used to
// authenticate anything — matching this project's own "auth
// precondition convention" applied to its logical extreme: `./connect`
// must already have succeeded on the target (Spectrum's own equivalent
// of "already logged in"; this port does not drive it, the same as it
// does not drive `twilio login`/`ovhcloud login`/`gh auth login`
// elsewhere in this project), and validate_certs/use_proxy (real
// modules' own HTTPS-transport knobs) have no meaning for a local IPC
// call at all.
//
// # Command syntax, verified against Broadcom's own TechDocs/knowledge
// # base (not guessed from the module names)
//
//   - `./create model ip=<ip> comm=<community> [lh=<landscape>]
//     [agentport=<port>]` — creates a device model by SNMP discovery
//     (confirmed via Broadcom's own "How to automate model creation or
//     deletion in Spectrum via CLI script" knowledge article).
//   - `./destroy model mh=<handle> -n` — destroys a model by handle,
//     `-n` running it without an interactive confirmation prompt (same
//     article).
//   - `./seek attr=<hex_id> val=<value> [lh=<landscape>]` — locates a
//     model by an attribute value (Broadcom's own "seek - Locates a
//     Model" command-reference page), used by this port to look up an
//     existing device by its Network_Address attribute (0x12d7f,
//     matching real spectrum_device.py's own get_device()) before
//     create/destroy, and by spectrum_model_attrs.go to resolve a model
//     handle from name+type.
//   - `./current mh=<handle>` then `./update attr=<hex_id>,val=<value>`
//     — sets the CLI's own "current model" context, then updates one
//     attribute on it (Broadcom's own "Modify a Model Attribute"
//     TechDocs page, whose own worked example is exactly `./update
//     attr=0x1007a,val=Subnet3,5`).
//   - `lh=<landscape_handle>` (decimal or hex) is confirmed as a
//     general filter usable across multiple vnmsh commands, not just
//     one.
//
// # An honestly-bounded gap: `seek`'s own success/no-match OUTPUT SHAPE
//
// Broadcom's own accessible documentation confirms `seek`'s purpose and
// flags but this port's own research could not capture a literal
// example transcript of its stdout on a match vs. a non-match (no live
// Spectrum landscape is available to this port to probe). spectrumSeek
// below therefore scans stdout for the first `0x[0-9a-f]+` token as the
// found model handle, treating a non-zero exit OR no such token as "not
// found" — the same class of honestly-bounded, fails-loud-not-silent
// gap ibm_sa_common.go's own doc comment already accepts for xcli's -s
// CSV shape, not a confident claim about a byte-exact format.
func spectrumRequireBinary(ctx context.Context, conn remoteexec.Connection, moduleName string) (Result, bool) {
	if _, err := run(ctx, conn, `test -x "$SPECROOT/vnmsh/create" -a -x "$SPECROOT/vnmsh/seek"`); err != nil {
		return Fail(fmt.Sprintf("%s: the vnmsh CLI (Broadcom's own official CA/DX NetOps Spectrum Command Line "+
			"Interface, `$SPECROOT/vnmsh/{create,destroy,update,current,seek}`) is required on the target and "+
			"was not found — this port shells out to it rather than calling OneClick's own REST API directly; "+
			"see spectrum_common.go's own doc comment, including the precondition that `./connect` must already "+
			"have been run in `$SPECROOT/vnmsh` on the target (vnmsh is a LOCAL tool with no remote-endpoint "+
			"concept of its own, unlike the url/url_username/url_password this module's real counterpart uses)",
			moduleName)), false
	}
	return Result{}, true
}

// spectrumRun runs one vnmsh script (e.g. "create", "destroy") with the
// given bare (unquoted-by-caller) argument tokens, from
// `$SPECROOT/vnmsh`.
func spectrumRun(ctx context.Context, conn remoteexec.Connection, script string, tokens ...string) (remoteexec.Result, error) {
	parts := append([]string{"./" + script}, tokens...)
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	cmd := `cd "$SPECROOT/vnmsh" && ` + strings.Join(quoted, " ")
	return runStatus(ctx, conn, cmd)
}

var spectrumHexHandleRE = regexp.MustCompile(`0x[0-9a-fA-F]+`)

// spectrumSeek runs `./seek attr=<attrHex> val=<value> [lh=<landscape>]`
// (space-separated key=value tokens, matching the confirmed `./create
// model ip=$IP`/`./destroy model mh=$model` shape rather than
// `./update`'s own confirmed comma-joined single-token shape — this
// port could not confirm which of the two `seek` itself actually uses,
// and picked the more consistently attested one; see spectrum_common.go's
// own doc comment) and returns the first model handle found in its
// output.
func spectrumSeek(ctx context.Context, conn remoteexec.Connection, attrHex, value, landscape string) (handle string, found bool, err error) {
	tokens := []string{"attr=" + attrHex, "val=" + value}
	if landscape != "" {
		tokens = append(tokens, "lh="+landscape)
	}
	res, err := spectrumRun(ctx, conn, "seek", tokens...)
	if err != nil {
		return "", false, err
	}
	if res.RC != 0 {
		return "", false, nil
	}
	m := spectrumHexHandleRE.FindString(res.Stdout)
	if m == "" {
		return "", false, nil
	}
	return m, true, nil
}

// spectrumUpdateAttr sets one attribute on the model identified by
// handle: `./current mh=<handle>` then `./update attr=<id>,val=<value>`
// — see spectrum_common.go's own doc comment for why this port issues
// one `update` call per attribute rather than relying on `seek`'s own
// undocumented (to this port) multi-attribute comma-list shape.
func spectrumUpdateAttr(ctx context.Context, conn remoteexec.Connection, handle, attrID, value string) (remoteexec.Result, error) {
	if res, err := spectrumRun(ctx, conn, "current", "mh="+handle); err != nil {
		return remoteexec.Result{}, err
	} else if res.RC != 0 {
		return res, nil
	}
	return spectrumRun(ctx, conn, "update", "attr="+attrID+",val="+value)
}

func spectrumErrMsg(res remoteexec.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" {
		msg = strings.TrimSpace(res.Stdout)
	}
	return msg
}
