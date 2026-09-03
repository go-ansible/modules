package modules

import (
	"context"
	"strings"

	remoteexec "github.com/go-remoteexec/transport"
)

// moduleEjabberdUser implements Ansible's `ejabberd_user`
// (community.general) module: manages an ejabberd XMPP server user
// account via the `ejabberdctl` command-line tool (real ejabberd_user's
// own mechanism too — REQUIREMENTS: "ejabberd with mod_admin_extra").
//
// Args: username (string, required); host (string, required) — the
// ejabberd virtual host; password (string, required if state=present);
// state (present|absent, default "present").
//
// State semantics, exactly matching real ejabberd_user's own
// EjabberdUser class: existence is checked via `ejabberdctl
// check_account <user> <host>` (RC 0 means the account exists).
// state=absent removes it via `ejabberdctl unregister <user> <host>` if
// it exists, else no-op. state=present: if the account does not exist,
// creates it via `ejabberdctl register <user> <host> <password>`; if it
// already exists, checks the given password against the account's
// current one via `ejabberdctl check_password <user> <host> <password>`
// (RC 0 means it already matches — no-op) and, if it doesn't match,
// updates it via `ejabberdctl change_password <user> <host>
// <password>`.
//
// Real ejabberd_user's own NOTES apply here unchanged: "Passwords must
// be stored in clear text for this release" — like real ejabberd_user,
// this port passes the password as a literal ejabberdctl command-line
// argument, with no alternative mechanism available.
func moduleEjabberdUser(ctx context.Context, conn remoteexec.Connection, args map[string]any) (Result, error) {
	username, err := requireString(args, "username")
	if err != nil {
		return Result{}, err
	}
	host, err := requireString(args, "host")
	if err != nil {
		return Result{}, err
	}
	state := argString(args, "state", "present")

	existsRes, err := runStatus(ctx, conn, "ejabberdctl check_account "+shellQuote(username)+" "+shellQuote(host))
	if err != nil {
		return Result{}, err
	}
	exists := existsRes.RC == 0

	switch state {
	case "absent":
		if !exists {
			return Ok(username + " already absent"), nil
		}
		res, err := runStatus(ctx, conn, "ejabberdctl unregister "+shellQuote(username)+" "+shellQuote(host))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("ejabberd_user: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(username + " unregistered"), nil

	case "present":
		password, err := requireString(args, "password")
		if err != nil {
			return Result{}, errArg("ejabberd_user: password is required when state=present")
		}
		if !exists {
			res, err := runStatus(ctx, conn, "ejabberdctl register "+shellQuote(username)+" "+shellQuote(host)+" "+shellQuote(password))
			if err != nil {
				return Result{}, err
			}
			if res.RC != 0 {
				return Fail("ejabberd_user: " + strings.TrimSpace(res.Stderr)), nil
			}
			return Changed(username + " registered"), nil
		}
		matchRes, err := runStatus(ctx, conn, "ejabberdctl check_password "+shellQuote(username)+" "+shellQuote(host)+" "+shellQuote(password))
		if err != nil {
			return Result{}, err
		}
		if matchRes.RC == 0 {
			return Ok(username + " already present"), nil
		}
		res, err := runStatus(ctx, conn, "ejabberdctl change_password "+shellQuote(username)+" "+shellQuote(host)+" "+shellQuote(password))
		if err != nil {
			return Result{}, err
		}
		if res.RC != 0 {
			return Fail("ejabberd_user: " + strings.TrimSpace(res.Stderr)), nil
		}
		return Changed(username + " password updated"), nil

	default:
		return Result{}, errArg("ejabberd_user: state must be present or absent, got %q", state)
	}
}
