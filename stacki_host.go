package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleStackiHost implements Ansible's `stacki_host` module: adds or
// removes a host on a Stacki front-end.
//
// # Architectural substitution: `stack` instead of the Stacki REST API
//
// Real stacki_host talks to a REMOTE Stacki front-end over its HTTP
// API from the control node: it does its own CSRF/session login dance
// (GET the endpoint for an initial csrftoken cookie, POST
// username/password to /login for a session cookie), then POSTs a
// single JSON body `{"cmd": "<command line>"}` to that same endpoint —
// this is the whole of Stacki's own REST API: a thin wrapper that
// takes the EXACT command line you would otherwise type at a `stack`
// shell prompt and runs it server-side (verified directly against real
// stacki_host.py's own stack_add/stack_remove/stack_force_install/
// stack_check_host methods, all of which build a `cmd` string
// identical to `stack`'s own CLI grammar — e.g. `"add host %s
// rack=%s rank=%s appliance=%s"`). Since that HTTP endpoint's own
// server-side behavior IS running `stack <cmd>` on the Stacki
// front-end, and this port's whole architecture is "reach the target
// through the Connection", this module simply shells out to the real
// `stack` binary directly on conn's target — which this port's
// Connection must therefore point AT the Stacki front-end node itself
// (where `stack` is always installed as a core part of the toolkit),
// not at some other host that would need its own HTTP client to reach
// a remote front-end. This reproduces the exact same server-side
// effect with no HTTP/CSRF/session layer needed at all.
//
// # Auth precondition / dead arguments
//
// stacki_user/stacki_password/stacki_endpoint are all still ACCEPTED
// (required, matching real stacki_host's own argument_spec, for
// argument-shape compatibility with real playbooks) but have NO EFFECT
// on this port's behavior: `stack` run locally on the front-end node
// needs no login at all (it's a local root-only CLI, not a network
// service this port must authenticate to) — a deliberate,
// honestly-documented gap, not a silent misinterpretation. This
// mirrors ipa_common.go's own "a prior `kinit`, not a per-invocation
// login" framing, taken one step further: here there is no
// per-invocation login concept whatsoever to wire up, because the
// whole HTTP/session layer real stacki_host builds is itself just a
// transport this port doesn't need.
//
// # Faithfully-reproduced real behavior, including one apparent bug
//
// Args: name (required); state (present|absent, default "present");
// appliance (default "backend"); rack (default 0); rank (default 0);
// network (default "private"; per real stacki_host's own doc,
// "Currently not used by the module"); prim_intf/prim_intf_ip/
// prim_intf_mac (per real stacki_host's own doc, all three "Currently
// not used by the module" either — EXCEPT that real stacki_host's own
// main() still requires all of appliance/rack/rank/prim_intf/
// prim_intf_ip/network/prim_intf_mac to be non-empty before it calls
// stack_add for a genuinely new host, even though four of those seven
// are then never actually read by stack_add itself. This port
// faithfully reproduces that exact validation gate (it's a real,
// verified-from-source quirk, not a guess) rather than silently
// dropping it because the three intf_* fields "look" unused.
// force_install (bool, default false).
//
// state=present, host already exists, force_install=true: runs `stack
// set host boot <name> action=install`, then `stack sync config` and
// `stack sync host config` (both always run after any mutating `stack`
// call, matching real stacki_host's own stack_sync() being called from
// every one of stack_add/stack_remove/stack_force_install). Changed is
// always true here, matching real stacki_host's own stack_force_install
// (it sets `changed = True` unconditionally right after the API call).
//
// state=present, host exists, force_install=false: no-op, Changed
// false, Msg explains force_install is needed to re-bootstrap — exactly
// matching real stacki_host's own message text.
//
// state=present, host does NOT exist: after the validation gate above,
// runs `stack add host <name> rack=<rack> rank=<rank>
// appliance=<appliance>` then syncs. Changed is FALSE here — this
// faithfully reproduces what reads as a genuine bug in real
// stacki_host.py's own stack_add(): it declares a local `changed =
// False` and never updates it before writing `result["changed"] =
// changed`, so a real ansible-playbook run against real stacki_host
// ALSO reports changed=false after successfully adding a brand-new
// host. This port matches that exactly per this project's own
// "faithfully reproduce a verified real quirk rather than silently
// improving on it" precedent (see pacemaker_cluster.go's own `force`
// dead-argument note for the same stance).
//
// state=absent, host exists: runs `stack remove host <name>` then
// syncs. Changed is true (real stacki_host DOES set this one
// correctly).
//
// state=absent, host does not exist: no-op, Changed false, no message.
//
// Host existence is checked the same way real stacki_host's own
// stack_check_host does: run `stack list host` (never filtered to one
// host — matching real stacki_host's own `{"cmd": "list host"}`, which
// takes no host argument either) and test whether the hostname appears
// anywhere in its combined output.
func moduleStackiHost(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")
	switch state {
	case "present", "absent":
	default:
		return Result{}, errArg("stacki_host: state must be one of present, absent, got %q", state)
	}
	// stacki_user/stacki_password/stacki_endpoint: accepted, no effect — see doc comment above.

	exists, err := stackiHostExists(ctx, conn, name)
	if err != nil {
		return Result{}, err
	}

	switch {
	case state == "present" && exists && argBool(args, "force_install", false):
		if _, err := runStatus(ctx, conn, "stack set host boot "+shellQuote(name)+" action=install"); err != nil {
			return Result{}, err
		}
		if err := stackiSync(ctx, conn); err != nil {
			return Result{}, err
		}
		return Changed("api call successful"), nil

	case state == "present" && exists:
		return Ok(name + " already exists. Set 'force_install' to true to bootstrap"), nil

	case state == "present" && !exists:
		// appliance/rack/rank/network all have non-empty real defaults
		// (backend/0/0/private), so — matching real stacki_host's own
		// validation gate exactly — only the three intf_* fields (no
		// default) can actually trigger this even though they, like
		// network, are otherwise unused by this port (and by real
		// stacki_host itself; see doc comment above).
		var missing []string
		for _, field := range []string{"prim_intf", "prim_intf_ip", "prim_intf_mac"} {
			if argString(args, field, "") == "" {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			return Result{}, errArg("stacki_host: missing required arguments: %s", strings.Join(missing, ", "))
		}
		appliance := argString(args, "appliance", "backend")
		rack := argInt(args, "rack", 0)
		rank := argInt(args, "rank", 0)
		cmd := "stack add host " + shellQuote(name) +
			" rack=" + shellQuote(fmtAny(rack)) +
			" rank=" + shellQuote(fmtAny(rank)) +
			" appliance=" + shellQuote(appliance)
		if _, err := runStatus(ctx, conn, cmd); err != nil {
			return Result{}, err
		}
		if err := stackiSync(ctx, conn); err != nil {
			return Result{}, err
		}
		// Changed=false here is a faithful reproduction of a real bug — see doc comment above.
		res := Ok("api call successful")
		return res, nil

	case state == "absent" && exists:
		if _, err := runStatus(ctx, conn, "stack remove host "+shellQuote(name)); err != nil {
			return Result{}, err
		}
		if err := stackiSync(ctx, conn); err != nil {
			return Result{}, err
		}
		return Changed("api call successful"), nil

	default: // state == "absent" && !exists
		return Ok(""), nil
	}
}

func stackiHostExists(ctx context.Context, conn remoteexec.Connection, name string) (bool, error) {
	res, err := runStatus(ctx, conn, "stack list host")
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, name), nil
}

func stackiSync(ctx context.Context, conn remoteexec.Connection) error {
	if _, err := runStatus(ctx, conn, "stack sync config"); err != nil {
		return err
	}
	if _, err := runStatus(ctx, conn, "stack sync host config"); err != nil {
		return err
	}
	return nil
}
